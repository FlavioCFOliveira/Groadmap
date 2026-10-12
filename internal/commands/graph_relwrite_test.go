package commands

// Tests for the relationship writes recorded in SPEC/GRAPH.md § What Groadmap
// Does Not Check, item 4, and asserted by acceptance criterion 38's third and
// sixth bullets.
//
// An earlier pinned engine removed a relationship property by the endpoint pair
// the expansion emitted, which carries the relationship the way the PATTERN
// walked it rather than the way storage holds it, so a `REMOVE e.k` through a
// pattern walking against the stored arrow was dropped — silently, with no
// counter. The pinned engine removes the property from every relationship the
// pattern bound, whichever way the pattern walked it, and counts each removal.
// The item is kept because it is a boundary an engine change can move back, and
// mitigation 3 of § Dependency Maturity Risk requires it re-measured in BOTH
// directions: a removal through an incoming pattern, and through an undirected
// one anchored on either endpoint, reaches every relationship the pattern bound;
// an assignment through the same patterns still persists; a DELETE over them
// still removes every relationship they matched; and the relationships a
// statement did not bind keep every property they held.
//
// A removal's reach was once decided by the DATA rather than by the statement,
// so the fixture fixes the stored orientation of every relationship it measures,
// and every assertion reads each relationship back through the outgoing pattern
// that matches how storage holds it.
//
// Groadmap hands the engine whatever it is given and holds no opinion about the
// patterns a statement binds. The tests below therefore assert the specified
// OUTCOME rather than a refusal: an absence of checking cannot be tested, and an
// outcome can, so a check reintroduced here fails these tests instead of passing
// them.

import (
	"encoding/json"
	"strings"
	"testing"
)

// The keys of the fixture's three nodes.
const (
	relWriteSpecKey = "graph-write-direction"
	relWriteTestKey = "graph_relwrite_test.go"
	relWriteCodeKey = "internal/commands/graph.go"
)

// storedEdge names one relationship of the fixture, and the OUTGOING pattern
// that binds `e` to it and to nothing else, as storage holds it.
type storedEdge struct {
	name     string
	outgoing string
}

// The relationships of the fixture: one on each side of the Test node, and a
// third joining the Test and CodeFile nodes in the direction opposite to the
// second, so that pair is joined in BOTH directions.
var (
	verifiedByEdge = storedEdge{
		name:     "the VERIFIED_BY relationship stored spec -> test",
		outgoing: "MATCH (s:Spec {key:'" + relWriteSpecKey + "'})-[e:VERIFIED_BY]->(v:Test)",
	}
	exercisesEdge = storedEdge{
		name:     "the EXERCISES relationship stored test -> code",
		outgoing: "MATCH (v:Test {key:'" + relWriteTestKey + "'})-[e:EXERCISES]->(c:CodeFile)",
	}
	coveredByEdge = storedEdge{
		name:     "the COVERED_BY relationship stored code -> test",
		outgoing: "MATCH (c:CodeFile {key:'" + relWriteCodeKey + "'})-[e:COVERED_BY]->(v:Test)",
	}
)

// relWriteStale is the reason every failure below names: each assertion is a
// measured property of the pinned engine, and a failure means the engine moved.
const relWriteStale = "the engine's behaviour has moved and SPEC/GRAPH.md § What Groadmap Does Not Check, " +
	"item 4, and acceptance criterion 38 no longer describe it; both must be corrected"

// relWriteEdges is every relationship of the fixture.
var relWriteEdges = []storedEdge{verifiedByEdge, exercisesEdge, coveredByEdge}

// seedProvenanceEdge creates the fixture the direction tests share: a Spec node,
// a Test node and a CodeFile node, a VERIFIED_BY relationship stored
// spec -> test, an EXERCISES relationship stored test -> code, and a COVERED_BY
// relationship stored code -> test. The Test node is the anchor, so its
// relationships are stored both arriving at it and leaving it, the Test and
// CodeFile pair is joined in both directions, and a property that reads back
// proves the write reached storage rather than a phantom pair.
func seedProvenanceEdge(t *testing.T, roadmap string) {
	t.Helper()
	for _, seed := range []string{
		"CREATE (:Spec {key:'" + relWriteSpecKey + "'}), (:Test {key:'" + relWriteTestKey + "'}), " +
			"(:CodeFile {key:'" + relWriteCodeKey + "'})",
		"MATCH (s:Spec {key:'" + relWriteSpecKey + "'}), (v:Test {key:'" + relWriteTestKey + "'}) " +
			"MERGE (s)-[:VERIFIED_BY]->(v)",
		"MATCH (v:Test {key:'" + relWriteTestKey + "'}), (c:CodeFile {key:'" + relWriteCodeKey + "'}) " +
			"MERGE (v)-[:EXERCISES]->(c)",
		"MATCH (v:Test {key:'" + relWriteTestKey + "'}), (c:CodeFile {key:'" + relWriteCodeKey + "'}) " +
			"MERGE (c)-[:COVERED_BY]->(v)",
	} {
		if err := runGraphClient([]string{"-r", roadmap, "--query", seed}); err != nil {
			t.Fatalf("seed %q: %v", seed, err)
		}
	}
}

// readEdgeProperty returns the value of key on one relationship of the fixture,
// read through the outgoing pattern that matches how it is stored, and reports
// whether the property is present (a JSON null reads as absent).
//
// The read-back is deliberately expressed with the OUTGOING pattern, which
// SPEC/GRAPH.md § What Groadmap Does Not Check item 5 states is correct whatever
// the data: the point is to observe storage, and a read through the pattern that
// wrote is the very thing whose agreement with the write is in question.
func readEdgeProperty(t *testing.T, roadmap string, edge storedEdge, key string) (string, bool) {
	t.Helper()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphClient([]string{"-r", roadmap, "--query",
			edge.outgoing + " RETURN e." + key}); err != nil {
			t.Fatalf("read back %q on %s: %v", key, edge.name, err)
		}
	})

	var parsed struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("read back %q on %s: stdout is not the columns/rows shape: %v\nstdout=%q",
			key, edge.name, err, stdout)
	}
	if len(parsed.Rows) != 1 {
		t.Fatalf("read back %q on %s: expected exactly one relationship row, got %d\nstdout=%q",
			key, edge.name, len(parsed.Rows), stdout)
	}
	if parsed.Rows[0][0] == nil {
		return "", false
	}
	s, ok := parsed.Rows[0][0].(string)
	if !ok {
		t.Fatalf("read back %q on %s: value is %T, want string", key, edge.name, parsed.Rows[0][0])
	}
	return s, true
}

// requireEdgeProperty fails the test unless key on edge reads back as want.
func requireEdgeProperty(t *testing.T, roadmap string, edge storedEdge, key, want, why string) {
	t.Helper()
	got, present := readEdgeProperty(t, roadmap, edge, key)
	if !present {
		t.Fatalf("%s: %s carries no %s; %s", why, edge.name, key, relWriteStale)
	}
	if got != want {
		t.Fatalf("%s: %s carries %s = %q, want %q; %s", why, edge.name, key, got, want, relWriteStale)
	}
}

// requireEdgePropertyAbsent fails the test unless key on edge reads back absent.
func requireEdgePropertyAbsent(t *testing.T, roadmap string, edge storedEdge, key, why string) {
	t.Helper()
	if got, present := readEdgeProperty(t, roadmap, edge, key); present {
		t.Fatalf("%s: %s still carries %s = %q; %s", why, edge.name, key, got, relWriteStale)
	}
}

// stampEvery assigns key = value on every relationship of the fixture through
// the outgoing pattern that holds it, so a removal that follows starts from a
// known state.
func stampEvery(t *testing.T, roadmap, key, value string) {
	t.Helper()
	for _, edge := range relWriteEdges {
		if err := runGraphClient([]string{"-r", roadmap, "--query",
			edge.outgoing + " SET e." + key + " = '" + value + "'"}); err != nil {
			t.Fatalf("stamping %s on %s: %v", key, edge.name, err)
		}
	}
}

// TestGraphUpdate_RelationshipWriteDirection is the core case: the relationships
// on both sides of one node, written and then removed through each pattern
// orientation. Every assignment persists and reads back; every removal reaches
// exactly the relationships its pattern bound, publishes a counter for each, and
// leaves every other relationship's property in place.
//
// The assignment subtests run in order and build on each other; each removal
// subtest stamps the fixture afresh first.
func TestGraphUpdate_RelationshipWriteDirection(t *testing.T) {
	const roadmap = "graph-relwrite-direction"
	defer servedRoadmap(t, roadmap)()
	seedProvenanceEdge(t, roadmap)

	// -- Source node, outgoing: the reach through the stored arrow --------------
	t.Run("from the source node, outgoing, writes and reads back", func(t *testing.T) {
		if err := runGraphClient([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'" + relWriteSpecKey + "'})-[e:VERIFIED_BY]->(v) " +
				"SET e.from_source = 'commit-aaa111'"}); err != nil {
			t.Fatalf("outgoing update from the source failed: %v", err)
		}
		requireEdgeProperty(t, roadmap, verifiedByEdge, "from_source", "commit-aaa111",
			"an outgoing assignment anchored on the source")
	})

	// -- Target node, outgoing: the same reach, anchored the other way ----------
	t.Run("from the target node, outgoing, writes and reads back", func(t *testing.T) {
		if err := runGraphClient([]string{"-r", roadmap, "--query",
			"MATCH (other)-[e:VERIFIED_BY]->(v:Test {key:'" + relWriteTestKey + "'}) " +
				"SET e.from_target = 'commit-bbb222'"}); err != nil {
			t.Fatalf("outgoing update anchored on the target failed: %v", err)
		}
		requireEdgeProperty(t, roadmap, verifiedByEdge, "from_target", "commit-bbb222",
			"an outgoing assignment anchored on the target must reach the relationships "+
				"ARRIVING at a node")
	})

	// -- Target node, incoming: acceptance criterion 38's third bullet ----------
	t.Run("from the target node, incoming, persists on every relationship it bound", func(t *testing.T) {
		stampEvery(t, roadmap, "last_commit", "commit-ccc333")
		out := graphClientJSON(t, roadmap,
			"MATCH (v:Test {key:'"+relWriteTestKey+"'})<-[e]-(s) SET e.last_commit = 'commit-ddd444'")
		if ok, _ := out["ok"].(bool); !ok {
			t.Fatalf("the incoming assignment must report success; got %v", out)
		}
		requireEdgeProperty(t, roadmap, verifiedByEdge, "last_commit", "commit-ddd444",
			"an assignment through an incoming pattern must persist")
		requireEdgeProperty(t, roadmap, coveredByEdge, "last_commit", "commit-ddd444",
			"an assignment through an incoming pattern must persist on every relationship it bound")
		// The incoming pattern bound only the relationships arriving at the
		// anchor, so the one leaving it is untouched.
		requireEdgeProperty(t, roadmap, exercisesEdge, "last_commit", "commit-ccc333",
			"an incoming pattern binds no relationship leaving the anchor")
	})

	// -- Target node, undirected: every incident relationship, both orientations -
	t.Run("from the target node, undirected, persists whichever way each is stored", func(t *testing.T) {
		out := graphClientJSON(t, roadmap,
			"MATCH (v:Test {key:'"+relWriteTestKey+"'})-[e]-(s) SET e.last_commit = 'commit-eee555'")
		if ok, _ := out["ok"].(bool); !ok {
			t.Fatalf("the undirected assignment must report success; got %v", out)
		}
		for _, edge := range relWriteEdges {
			requireEdgeProperty(t, roadmap, edge, "last_commit", "commit-eee555",
				"an undirected assignment must reach every relationship incident to the anchor")
		}
	})

	// -- Target node, incoming removal: acceptance criterion 38's sixth bullet ---
	t.Run("from the target node, incoming, a removal reaches every relationship it bound", func(t *testing.T) {
		stampEvery(t, roadmap, "last_commit", "commit-fff666")
		out := graphClientJSON(t, roadmap,
			"MATCH (v:Test {key:'"+relWriteTestKey+"'})<-[e]-(s) REMOVE e.last_commit")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 2},
			"a removal through an incoming pattern that bound two relationships")
		requireEdgePropertyAbsent(t, roadmap, verifiedByEdge, "last_commit",
			"a removal through an incoming pattern must remove the property")
		requireEdgePropertyAbsent(t, roadmap, coveredByEdge, "last_commit",
			"a removal through an incoming pattern must remove the property from every relationship it bound")
		requireEdgeProperty(t, roadmap, exercisesEdge, "last_commit", "commit-fff666",
			"a relationship pointing away from the anchor is not bound by an incoming pattern")
	})

	// -- The same removal, outgoing: the form that held under both engines -------
	t.Run("from the target node, outgoing, a removal removes the property", func(t *testing.T) {
		stampEvery(t, roadmap, "last_commit", "commit-ggg777")
		out := graphClientJSON(t, roadmap,
			"MATCH (s)-[e]->(v:Test {key:'"+relWriteTestKey+"'}) REMOVE e.last_commit")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 2},
			"a removal through an outgoing pattern arriving at the anchor")
		requireEdgePropertyAbsent(t, roadmap, verifiedByEdge, "last_commit",
			"a removal through an outgoing pattern must remove the property")
		requireEdgePropertyAbsent(t, roadmap, coveredByEdge, "last_commit",
			"a removal through an outgoing pattern must remove the property")
		requireEdgeProperty(t, roadmap, exercisesEdge, "last_commit", "commit-ggg777",
			"a pattern arriving at the anchor binds no relationship leaving it")
	})

	// -- Undirected removals, anchored on either endpoint -------------------------
	t.Run("undirected, anchored on the test node, a removal reaches every incident relationship", func(t *testing.T) {
		stampEvery(t, roadmap, "last_commit", "commit-hhh888")
		out := graphClientJSON(t, roadmap,
			"MATCH (v:Test {key:'"+relWriteTestKey+"'})-[e]-(x) REMOVE e.last_commit")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 3},
			"an undirected removal anchored on the test node")
		for _, edge := range relWriteEdges {
			requireEdgePropertyAbsent(t, roadmap, edge, "last_commit",
				"an undirected removal must reach every relationship it bound, whichever way each is stored")
		}
	})
	t.Run("undirected, anchored on the code file, a removal reaches both relationships of the pair", func(t *testing.T) {
		stampEvery(t, roadmap, "last_commit", "commit-iii999")
		out := graphClientJSON(t, roadmap,
			"MATCH (c:CodeFile {key:'"+relWriteCodeKey+"'})-[e]-(x) REMOVE e.last_commit")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 2},
			"an undirected removal anchored on the other endpoint")
		requireEdgePropertyAbsent(t, roadmap, exercisesEdge, "last_commit",
			"an undirected removal walking against the stored arrow must still land")
		requireEdgePropertyAbsent(t, roadmap, coveredByEdge, "last_commit",
			"an undirected removal must reach every relationship it bound")
		requireEdgeProperty(t, roadmap, verifiedByEdge, "last_commit", "commit-iii999",
			"a relationship not incident to the anchor keeps its property")
	})

	// -- A pair joined in both directions, with and without a WHERE -------------
	t.Run("over a pair joined in both directions, a WHERE selecting one leg removes from it alone", func(t *testing.T) {
		stampEvery(t, roadmap, "stamp", "reviewed")
		out := graphClientJSON(t, roadmap,
			"MATCH (n)-[e]-(m {key:'"+relWriteCodeKey+"'}) WHERE startNode(e).key = '"+relWriteCodeKey+"' REMOVE e.stamp")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 1},
			"a removal behind a WHERE that selects one relationship of the pair")
		requireEdgePropertyAbsent(t, roadmap, coveredByEdge, "stamp",
			"the relationship the WHERE selected must lose the property")
		requireEdgeProperty(t, roadmap, exercisesEdge, "stamp", "reviewed",
			"the relationship the WHERE did not select keeps its property")
	})
	t.Run("over a pair joined in both directions, a removal reaches both legs", func(t *testing.T) {
		stampEvery(t, roadmap, "stamp", "reviewed-again")
		out := graphClientJSON(t, roadmap,
			"MATCH (v:Test {key:'"+relWriteTestKey+"'})-[e]-(c:CodeFile {key:'"+relWriteCodeKey+"'}) REMOVE e.stamp")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 2},
			"a removal over a pair joined in both directions")
		requireEdgePropertyAbsent(t, roadmap, exercisesEdge, "stamp", "the outgoing leg must lose the property")
		requireEdgePropertyAbsent(t, roadmap, coveredByEdge, "stamp", "the incoming leg must lose the property")
		requireEdgeProperty(t, roadmap, verifiedByEdge, "stamp", "reviewed-again",
			"a relationship outside the pair keeps its property")
	})
}

// TestGraphDelete_UndirectedBareDeleteRemovesTheEdge pins the shape the hazard
// does NOT reach. A bare DELETE over an undirected pattern removes every
// relationship it matched, whichever way each is stored (SPEC/GRAPH.md § What
// Groadmap Does Not Check, items 4 and 5).
func TestGraphDelete_UndirectedBareDeleteRemovesTheEdge(t *testing.T) {
	const roadmap = "graph-relwrite-scope"
	defer servedRoadmap(t, roadmap)()
	seedProvenanceEdge(t, roadmap)

	if err := runGraphClient([]string{"-r", roadmap, "--query",
		"MATCH (v:Test {key:'" + relWriteTestKey + "'})-[e]-(x) DELETE e"}); err != nil {
		t.Fatalf("undirected delete failed: %v", err)
	}
	for _, edge := range relWriteEdges {
		stdout, _ := captureStdStreams(t, func() {
			if err := runGraphClient([]string{"-r", roadmap, "--query",
				edge.outgoing + " RETURN e"}); err != nil {
				t.Fatalf("post-delete read of %s: %v", edge.name, err)
			}
		})
		var parsed struct {
			Rows [][]any `json:"rows"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
			t.Fatalf("post-delete read is not the columns/rows shape: %v\nstdout=%q", err, stdout)
		}
		if len(parsed.Rows) != 0 {
			t.Errorf("undirected delete reported success but %s survived: %s", edge.name, stdout)
		}
	}
}
