package commands

// Tests for the `counters` member `rmp graph client` publishes beside a
// statement's result (SPEC/DATA_FORMATS.md § Graph Query Counters;
// SPEC/GRAPH.md § Write Counters: What a Statement Changed).
//
// # What the defect was, and why the values matter more than the presence
//
// Every writing statement printed `{"ok": true}` and nothing else. The engine
// had tracked the write effects all along and rmp read none of them, so two
// invocations that both printed the same line may have created a node and
// matched an existing one, or deleted a thousand relationships and deleted
// none. A test that only asserted the member's PRESENCE would pass on an
// implementation that published a constant, which is why every case below
// asserts the whole block — its members and their values — and not merely that
// something arrived.
//
// # The three shapes of failure this file is built against
//
//  1. A block published where none is due. Rule 1 makes the member present if
//     and only if the statement changed something, and an over-eager mapping
//     changes the bytes of every read and every no-op write. Asserted with
//     complete key sets, so a stray member cannot hide.
//  2. A zero published rather than omitted. Rule 2 omits it, and asserting the
//     exact member set is what catches ten published zeroes.
//  3. A figure that is a constant, an echo of another figure, or the wrong
//     counter. Every fixture below is chosen so the expected numbers could not
//     have been produced by a simpler rule: two properties on a one-node CREATE,
//     one relationship deleted by a DETACH DELETE that names only the node, a
//     property figure of 2 for a statement that assigns once and removes once.

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// graphClientJSON runs one statement through `rmp graph client` and returns
// its decoded stdout. It fails the test if the statement errors, because every
// statement in this file is one the engine accepts.
func graphClientJSON(t *testing.T, roadmap, query string) map[string]any {
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

// graphClientRaw runs one statement and returns its stdout verbatim, for the
// assertions whose subject is the BYTES rather than the decoded object.
func graphClientRaw(t *testing.T, roadmap, query string) string {
	t.Helper()

	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphClient([]string{"-r", roadmap, "--query", query}); err != nil {
			t.Fatalf("statement failed: %v\nquery=%s", err, query)
		}
	})
	return stdout
}

// countersOfOutput reads the `counters` member out of a decoded result, and
// reports whether the key was there at all. The two are different answers and
// the caller always distinguishes them: an absent key is rule 1's guarantee, and
// an empty object is the state rule 2 says never occurs.
func countersOfOutput(t *testing.T, out map[string]any) (map[string]any, bool) {
	t.Helper()

	raw, present := out["counters"]
	if !present {
		return nil, false
	}
	block, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("`counters` must be a JSON object.\ngot: %#v", raw)
	}
	return block, true
}

// assertCounters requires the published block to be EXACTLY want: the same
// members, with the same values, and no others.
//
// Exactness is the point. A published zero, a counter attributed to the wrong
// effect, and a member the specification does not name all fail here, and none
// of them would fail a check that merely looked up the keys it expected.
func assertCounters(t *testing.T, out map[string]any, want map[string]int64, context string) {
	t.Helper()

	block, present := countersOfOutput(t, out)
	if !present {
		t.Fatalf("%s: the statement changed the graph, so it must publish a `counters` "+
			"member (SPEC/DATA_FORMATS.md § Graph Query Counters, rule 1).\ngot: %v", context, out)
	}
	if len(block) == 0 {
		t.Fatalf("%s: the block is never empty when it is present, because a zero is "+
			"omitted and something changed (rule 2).\ngot: %v", context, out)
	}

	for key, wantValue := range want {
		raw, found := block[key]
		if !found {
			t.Errorf("%s: expected %s = %d, but the key is absent.\ngot: %v",
				context, key, wantValue, block)
			continue
		}
		// Every counter is a JSON number; encoding/json decodes it as float64.
		got, ok := raw.(float64)
		if !ok {
			t.Errorf("%s: %s must be a JSON number.\ngot: %#v", context, key, raw)
			continue
		}
		if int64(got) != wantValue {
			t.Errorf("%s: %s = %v, want %d", context, key, raw, wantValue)
		}
	}

	gotKeys := slices.Sorted(maps.Keys(block))
	wantKeys := slices.Sorted(maps.Keys(want))
	if !slices.Equal(gotKeys, wantKeys) {
		t.Errorf("%s: the published block must carry exactly the counters the statement "+
			"produced — a zero is omitted, not published (rule 2).\n"+
			"published: %v\nexpected:  %v", context, gotKeys, wantKeys)
	}
}

// assertNoCounters requires the key to be absent entirely, which is the
// guarantee every existing consumer depends on.
func assertNoCounters(t *testing.T, out map[string]any, context string) {
	t.Helper()

	if block, present := countersOfOutput(t, out); present {
		t.Errorf("%s: a statement that changed nothing carries no `counters` key at all, "+
			"and produces exactly the bytes it produced before the member existed "+
			"(SPEC/DATA_FORMATS.md § Graph Query Counters, rule 1).\ngot: %v", context, block)
	}
}

// TestGraphCounters_AWriteReportsWhatItChanged is the headline case, and the one
// SPEC/GRAPH.md acceptance criterion 61 states verbatim: three members and no
// fourth.
//
// Two properties on one node is deliberate. A property figure equal to the node
// count would be an echo rather than a count, and 2 against 1 tells them apart.
func TestGraphCounters_AWriteReportsWhatItChanged(t *testing.T) {
	const roadmap = "graph-counters-create"
	defer servedRoadmap(t, roadmap)()

	out := graphClientJSON(t, roadmap, "CREATE (:Widget {serial:'A-1', batch:7})")

	if ok, _ := out["ok"].(bool); !ok {
		t.Errorf("`ok` keeps its meaning, its value and its position: the member is "+
			"additive (SPEC/DATA_FORMATS.md § Graph Write Result).\ngot: %v", out)
	}
	assertCounters(t, out, map[string]int64{
		"nodesCreated":      1,
		"propertiesWritten": 2,
		"labelsAdded":       1,
	}, "CREATE of a labelled node with two properties")

	// The envelope is `ok` and the counters, and nothing else.
	gotKeys := slices.Sorted(maps.Keys(out))
	if !slices.Equal(gotKeys, []string{"counters", "ok"}) {
		t.Errorf("a write with no RETURN publishes exactly `ok` and `counters`.\ngot: %v", gotKeys)
	}
}

// TestGraphCounters_AReadIsUnchangedInEveryByte is criterion 61's second half,
// and the one an over-eager implementation breaks silently.
//
// It is asserted on the raw bytes rather than on the decoded object because the
// guarantee is about the bytes: an existing consumer parses them.
func TestGraphCounters_AReadIsUnchangedInEveryByte(t *testing.T) {
	const roadmap = "graph-counters-read"
	defer servedRoadmap(t, roadmap)()

	if err := runGraphClient([]string{"-r", roadmap, "--query",
		"CREATE (:Widget {serial:'A-1', batch:7})"}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	raw := graphClientRaw(t, roadmap, "MATCH (w:Widget) RETURN w.serial")
	const want = "{\n  \"columns\": [\n    \"w.serial\"\n  ],\n  \"rows\": [\n    [\n      \"A-1\"\n    ]\n  ]\n}\n"
	if raw != want {
		t.Errorf("a read changes nothing, so its output carries no `counters` key and is "+
			"unchanged in every byte.\ngot:  %q\nwant: %q", raw, want)
	}
}

// TestGraphCounters_APropertyWriteIsOneFigureCoveringTwoEffects is the fold, and
// the case SPEC/GRAPH.md acceptance criterion 63 singles out.
//
// The three statements are run against one graph in order because they build on
// each other, and the mixed one — assigning and removing in a single pass — is
// the one a pure SET would not have caught: an implementation that read only the
// engine's PropertiesSet publishes 1 for it where the specification requires 2.
func TestGraphCounters_APropertyWriteIsOneFigureCoveringTwoEffects(t *testing.T) {
	const roadmap = "graph-counters-properties"
	defer servedRoadmap(t, roadmap)()

	if err := runGraphClient([]string{"-r", roadmap, "--query",
		"CREATE (:Widget {serial:'A-1', batch:7})"}); err != nil {
		t.Fatalf("seeding the widget: %v", err)
	}
	if err := runGraphClient([]string{"-r", roadmap, "--query",
		"CREATE (:Gauge {serial:'G-1', reading:1, spare:'x'})"}); err != nil {
		t.Fatalf("seeding the gauge: %v", err)
	}

	assignment := graphClientJSON(t, roadmap, "MATCH (w:Widget {serial:'A-1'}) SET w.batch = 9")
	assertCounters(t, assignment, map[string]int64{"propertiesWritten": 1},
		"a SET that assigned one property")

	removal := graphClientJSON(t, roadmap, "MATCH (w:Widget {serial:'A-1'}) REMOVE w.batch")
	assertCounters(t, removal, map[string]int64{"propertiesWritten": 1},
		"a REMOVE of one property, folded under the same key")

	mixed := graphClientJSON(t, roadmap,
		"MATCH (g:Gauge {serial:'G-1'}) SET g.reading = 12 REMOVE g.spare")
	assertCounters(t, mixed, map[string]int64{"propertiesWritten": 2},
		"a statement that assigns one property and removes another in one pass")
}

// TestGraphCounters_AMergeThatMatchedIsDistinguishableFromOneThatCreated is
// acceptance criterion 62, and it compares two runs of the SAME statement,
// because it is the difference between them that a caller reads.
func TestGraphCounters_AMergeThatMatchedIsDistinguishableFromOneThatCreated(t *testing.T) {
	const roadmap = "graph-counters-merge"
	defer servedRoadmap(t, roadmap)()

	const merge = "MERGE (:Widget {serial:'B-2'})"

	created := graphClientJSON(t, roadmap, merge)
	assertCounters(t, created, map[string]int64{
		"nodesCreated":      1,
		"propertiesWritten": 1,
		"labelsAdded":       1,
	}, "the first run of a MERGE, which created")

	matched := graphClientJSON(t, roadmap, merge)
	assertNoCounters(t, matched, "the second run of the SAME MERGE, which matched")

	// And the second run's bytes are the bytes a write published before the
	// member existed: this is what the additive guarantee amounts to.
	raw := graphClientRaw(t, roadmap, merge)
	if raw != "{\n  \"ok\": true\n}\n" {
		t.Errorf("a MERGE that matched publishes exactly the {\"ok\": true} it published "+
			"before the counters existed.\ngot: %q", raw)
	}
}

// TestGraphCounters_ADeleteThatMatchedNoRowReportsNothing is criterion 62's
// other no-op class. It is separate from the MERGE because the two reach the
// same outcome by different routes: the MERGE applied nothing because the
// element existed, the DELETE because the pattern bound no row at all.
func TestGraphCounters_ADeleteThatMatchedNoRowReportsNothing(t *testing.T) {
	const roadmap = "graph-counters-delete-noop"
	defer servedRoadmap(t, roadmap)()

	if err := runGraphClient([]string{"-r", roadmap, "--query",
		"CREATE (:Widget {serial:'A-1'})"}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	out := graphClientJSON(t, roadmap, "MATCH (w:Widget {serial:'NOT-PRESENT'}) DELETE w")
	assertNoCounters(t, out, "a DELETE whose pattern matched no row")

	// The node the fixture did seed is still there, so the statement really was
	// a no-op rather than a delete that reported nothing.
	count := graphClientJSON(t, roadmap, "MATCH (w:Widget) RETURN count(w)")
	rows, _ := count["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("count query returned no row: %v", count)
	}
	first, _ := rows[0].([]any)
	if len(first) != 1 || first[0].(float64) != 1 {
		t.Errorf("the seeded node must survive a DELETE that matched nothing; got %v", first)
	}
}

// TestGraphCounters_ADetachDeleteReportsTheEdgeItRemovedAndNoPropertyFigure is
// criterion 62's third clause.
//
// Two things are proved at once and both are load-bearing. The relationship is
// counted although the statement names only the node — a DETACH DELETE removes
// it as a CONSEQUENCE, and a count taken from the statement's text rather than
// from the write path would miss it. And no property figure appears: deleting an
// element counts no property removal, so an implementation that swept the
// deleted element's properties into propertiesWritten shows a figure here.
func TestGraphCounters_ADetachDeleteReportsTheEdgeItRemovedAndNoPropertyFigure(t *testing.T) {
	const roadmap = "graph-counters-detach-delete"
	defer servedRoadmap(t, roadmap)()

	// The node carries two properties, so a property figure — if one were
	// wrongly published — would be visible rather than coincidentally zero.
	for _, seed := range []string{
		"CREATE (:Widget {serial:'C-3', batch:4})",
		"CREATE (:Gauge {serial:'G-9'})",
		"MATCH (w:Widget {serial:'C-3'}), (g:Gauge {serial:'G-9'}) CREATE (w)-[:MEASURED_BY]->(g)",
	} {
		if err := runGraphClient([]string{"-r", roadmap, "--query", seed}); err != nil {
			t.Fatalf("seeding %q: %v", seed, err)
		}
	}

	out := graphClientJSON(t, roadmap, "MATCH (w:Widget {serial:'C-3'}) DETACH DELETE w")
	assertCounters(t, out, map[string]int64{
		"nodesDeleted":         1,
		"relationshipsDeleted": 1,
	}, "a DETACH DELETE of a connected node carrying two properties")
}

// TestGraphCounters_ASchemaStatementReportsItsIndex covers the schema counters,
// which are the class that reaches the block through a statement carrying no
// pattern at all.
func TestGraphCounters_ASchemaStatementReportsItsIndex(t *testing.T) {
	const roadmap = "graph-counters-schema"
	defer servedRoadmap(t, roadmap)()

	added := graphClientJSON(t, roadmap, "CREATE INDEX widget_serial FOR (w:Widget) ON (w.serial)")
	assertCounters(t, added, map[string]int64{"indexesAdded": 1},
		"a CREATE INDEX that registered one index")

	dropped := graphClientJSON(t, roadmap, "DROP INDEX widget_serial")
	assertCounters(t, dropped, map[string]int64{"indexesRemoved": 1},
		"a DROP INDEX that dropped one index")
}

// TestGraphCounters_AWriteWithAReturnCarriesThemBesideItsRows is the second
// published envelope: the member is additive to the {columns, rows} shape too,
// and is written after rows.
func TestGraphCounters_AWriteWithAReturnCarriesThemBesideItsRows(t *testing.T) {
	const roadmap = "graph-counters-return"
	defer servedRoadmap(t, roadmap)()

	if err := runGraphClient([]string{"-r", roadmap, "--query",
		"CREATE (:Widget {serial:'A-1', batch:7})"}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	raw := graphClientRaw(t, roadmap,
		"MATCH (w:Widget {serial:'A-1'}) SET w.batch = 3 RETURN w.serial")

	var out map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\nstdout=%q", err, raw)
	}
	assertCounters(t, out, map[string]int64{"propertiesWritten": 1},
		"a SET ... RETURN")

	// columns and rows keep their meanings and their values: the member displaces
	// nothing.
	cols, _ := out["columns"].([]any)
	if len(cols) != 1 || cols[0] != "w.serial" {
		t.Errorf("`columns` is unchanged by the added member.\ngot: %v", out["columns"])
	}
	rows, _ := out["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("`rows` is unchanged by the added member.\ngot: %v", out["rows"])
	}

	// And `counters` is written AFTER `rows`, which is the position rule 3 fixes
	// and which the decoded map cannot show.
	if strings.Index(raw, `"rows"`) > strings.Index(raw, `"counters"`) {
		t.Errorf("`counters` is written after `rows`.\ngot: %s", raw)
	}
}

// TestGraphCounters_APrefixedStatementPublishesNone closes the mutual exclusion
// of rule 5 from the side this project controls.
//
// An EXPLAIN executes nothing, so it has no applied effect to count — and the
// assertion is worth making because the statement it prefixes IS a write, which
// is exactly the case an implementation reading the counters before deciding
// would get wrong.
func TestGraphCounters_APrefixedStatementPublishesNone(t *testing.T) {
	const roadmap = "graph-counters-explain"
	defer servedRoadmap(t, roadmap)()

	out := graphClientJSON(t, roadmap, "EXPLAIN CREATE (:Widget {serial:'NEVER'})")
	assertNoCounters(t, out, "an EXPLAIN of a writing statement")
	if _, planned := out["plan"]; !planned {
		t.Fatalf("the EXPLAIN must still publish its plan.\ngot: %v", out)
	}

	// A read written with PROFILE is the other prefixed class that reaches a
	// result. It executed, and it changed nothing, so it carries a profile tree
	// and no counters.
	profiled := graphClientJSON(t, roadmap, "PROFILE MATCH (w:Widget) RETURN w.serial")
	assertNoCounters(t, profiled, "a PROFILE of a read")
	if _, measured := profiled["profile"]; !measured {
		t.Fatalf("the PROFILE must still publish its measurement.\ngot: %v", profiled)
	}
}

// TestGraphCounters_TheEngineRefusesAProfileOfAWrite is the other half of rule
// 5's mutual exclusion, and it is an assertion about the ENGINE rather than
// about this code: neither surface has to arrange the exclusion, because a
// PROFILE of a writing statement never produces a result at all.
//
// It is pinned here because the specification rests on it. Were the engine to
// start executing such a statement, `counters` and `profile` could appear in one
// object and rule 5's guarantee to consumers would be silently false.
func TestGraphCounters_TheEngineRefusesAProfileOfAWrite(t *testing.T) {
	const roadmap = "graph-counters-profile-write"
	defer servedRoadmap(t, roadmap)()

	var err error
	_, _ = captureStdStreams(t, func() {
		err = runGraphClient([]string{"-r", roadmap, "--query", "PROFILE CREATE (:Widget {serial:'X'})"})
	})
	if err == nil {
		t.Fatal("the engine refuses a PROFILE of a writing statement rather than perform a " +
			"write a caller asked only to have measured; if it stopped refusing, " +
			"`counters` and `profile` could appear together and " +
			"SPEC/DATA_FORMATS.md § Graph Query Counters rule 5 would be false")
	}

	// And nothing was written, which is what makes the refusal a refusal.
	count := graphClientJSON(t, roadmap, "MATCH (w:Widget) RETURN count(w)")
	rows, _ := count["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("count query returned no row: %v", count)
	}
	first, _ := rows[0].([]any)
	if len(first) != 1 || first[0].(float64) != 0 {
		t.Errorf("a refused PROFILE must have written nothing; got %v", first)
	}
}
