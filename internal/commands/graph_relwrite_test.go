package commands

// Tests for the relationship-removal hazard published as SPEC/GRAPH.md § What
// Groadmap Does Not Check, item 4, and asserted by acceptance criterion 38's
// third and sixth bullets.
//
// The engine removes a relationship property by its endpoint PAIR, and it takes
// that pair from the columns the expansion emitted. Those columns carry the
// relationship the way the pattern walked it, not the way storage holds it, so a
// `REMOVE e.k` over a relationship reached against the stored arrow is addressed
// as a pair that holds no relationship. Nothing is removed, no error is raised,
// the transaction still commits, and no write counter is published, because a
// removal is counted only where it is applied.
//
// THE ASSIGNMENT FORMS ARE NOT AFFECTED, AND THAT IS HALF OF WHAT IS ASSERTED.
// The engine turns the pair into the stored orientation before an assignment and
// does not before a removal, so `SET e.k = …` reaches the relationship the
// pattern bound whichever way the pattern walked it. That boundary is the part of
// item 4 an engine upgrade is most likely to move, in either direction, so both
// sides of it are asserted here: an assignment through an incoming and through an
// undirected pattern persists, and a removal through an incoming pattern is
// dropped while the same removal through an outgoing pattern lands
// (SPEC/GRAPH.md § Dependency Maturity Risk, mitigation 3).
//
// How much of a removal survives is decided by the DATA rather than by the
// statement, so the fixture fixes the stored orientation of every relationship it
// measures — one stored INTO the anchor and one stored OUT of it — and every
// assertion reads each relationship back through the outgoing pattern that
// matches how storage holds it.
//
// Groadmap used to REFUSE the statements this hazard concerns. It no longer
// does: it hands the engine whatever it is given and holds no opinion about the
// patterns a statement binds. The tests below therefore assert the specified
// OUTCOME rather than a refusal. That direction is deliberate: an absence of
// checking cannot be tested, and an outcome can, so a check reintroduced here
// fails these tests instead of passing them.

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

// The two relationships of the fixture, one on each side of the Test node.
var (
	verifiedByEdge = storedEdge{
		name:     "the VERIFIED_BY relationship stored spec -> test",
		outgoing: "MATCH (s:Spec {key:'" + relWriteSpecKey + "'})-[e:VERIFIED_BY]->(v:Test)",
	}
	exercisesEdge = storedEdge{
		name:     "the EXERCISES relationship stored test -> code",
		outgoing: "MATCH (v:Test {key:'" + relWriteTestKey + "'})-[e:EXERCISES]->(c:CodeFile)",
	}
)

// relWriteStale is the reason every failure below names: each assertion is a
// measured property of the pinned engine, and a failure means the engine moved.
const relWriteStale = "the engine's behaviour has moved and SPEC/GRAPH.md § What Groadmap Does Not Check, " +
	"item 4, and acceptance criterion 38 no longer describe it; both must be corrected"

// seedProvenanceEdge creates the fixture the direction tests share: a Spec node,
// a Test node and a CodeFile node, a VERIFIED_BY relationship stored
// spec -> test, and an EXERCISES relationship stored test -> code. The Test node
// is the anchor, so one of its relationships is stored arriving at it and the
// other leaving it, and a property that reads back proves the write reached
// storage rather than a phantom pair.
func seedProvenanceEdge(t *testing.T, roadmap string) {
	t.Helper()
	for _, seed := range []string{
		"CREATE (:Spec {key:'" + relWriteSpecKey + "'}), (:Test {key:'" + relWriteTestKey + "'}), " +
			"(:CodeFile {key:'" + relWriteCodeKey + "'})",
		"MATCH (s:Spec {key:'" + relWriteSpecKey + "'}), (v:Test {key:'" + relWriteTestKey + "'}) " +
			"MERGE (s)-[:VERIFIED_BY]->(v)",
		"MATCH (v:Test {key:'" + relWriteTestKey + "'}), (c:CodeFile {key:'" + relWriteCodeKey + "'}) " +
			"MERGE (v)-[:EXERCISES]->(c)",
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

// TestGraphUpdate_RelationshipWriteDirection is the core case: the relationships
// on both sides of one node, written and then removed through each pattern
// orientation. Every assignment persists and reads back; the removal through the
// incoming pattern reports success, publishes no counters and removes nothing,
// and the same removal through an outgoing pattern removes the property.
//
// The subtests run in order and build on each other: the assignments set the
// value the removals then act on.
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
		out := graphClientJSON(t, roadmap,
			"MATCH (v:Test {key:'"+relWriteTestKey+"'})<-[e]-(s) SET e.last_commit = 'commit-ddd444'")
		if ok, _ := out["ok"].(bool); !ok {
			t.Fatalf("the incoming assignment must report success; got %v", out)
		}
		requireEdgeProperty(t, roadmap, verifiedByEdge, "last_commit", "commit-ddd444",
			"an assignment through an incoming pattern must persist (item 4: the assignment "+
				"forms are not affected)")
		// The incoming pattern bound only the relationship arriving at the anchor,
		// so the one leaving it is untouched — which is what makes the assertion
		// above about the relationship the pattern bound rather than about every
		// relationship of the node.
		requireEdgePropertyAbsent(t, roadmap, exercisesEdge, "last_commit",
			"an incoming pattern binds no relationship leaving the anchor")
	})

	// -- Target node, undirected: every incident relationship, both orientations -
	t.Run("from the target node, undirected, persists whichever way each is stored", func(t *testing.T) {
		out := graphClientJSON(t, roadmap,
			"MATCH (v:Test {key:'"+relWriteTestKey+"'})-[e]-(s) SET e.last_commit = 'commit-eee555'")
		if ok, _ := out["ok"].(bool); !ok {
			t.Fatalf("the undirected assignment must report success; got %v", out)
		}
		requireEdgeProperty(t, roadmap, verifiedByEdge, "last_commit", "commit-eee555",
			"an undirected assignment must reach the relationship stored arriving at the anchor")
		requireEdgeProperty(t, roadmap, exercisesEdge, "last_commit", "commit-eee555",
			"an undirected assignment must reach the relationship stored leaving the anchor")
	})

	// -- Target node, incoming removal: acceptance criterion 38's sixth bullet ---
	t.Run("from the target node, incoming, a removal reports success, publishes no counters and removes nothing", func(t *testing.T) {
		out := graphClientJSON(t, roadmap,
			"MATCH (v:Test {key:'"+relWriteTestKey+"'})<-[e]-(s) REMOVE e.last_commit")
		if ok, _ := out["ok"].(bool); !ok {
			t.Fatalf("the statement must EXECUTE and report success: Groadmap does not inspect "+
				"the patterns a statement binds (SPEC/GRAPH.md § What Groadmap Does Not Check, "+
				"item 4); got %v", out)
		}
		assertNoCounters(t, out,
			"a removal dropped because the pattern walked against the stored arrow")
		requireEdgeProperty(t, roadmap, verifiedByEdge, "last_commit", "commit-eee555",
			"a removal through an incoming pattern is expected to remove NOTHING")
		requireEdgeProperty(t, roadmap, exercisesEdge, "last_commit", "commit-eee555",
			"an incoming pattern binds no relationship leaving the anchor")
	})

	// -- The same removal, outgoing: the reach the hazard does not cost ----------
	t.Run("from the target node, outgoing, a removal removes the property", func(t *testing.T) {
		out := graphClientJSON(t, roadmap,
			"MATCH (s)-[e]->(v:Test {key:'"+relWriteTestKey+"'}) REMOVE e.last_commit")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 1},
			"a removal through an outgoing pattern, which lands")
		requireEdgePropertyAbsent(t, roadmap, verifiedByEdge, "last_commit",
			"a removal through an outgoing pattern must remove the property")
		requireEdgeProperty(t, roadmap, exercisesEdge, "last_commit", "commit-eee555",
			"a pattern arriving at the anchor binds no relationship leaving it")
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
	for _, edge := range []storedEdge{verifiedByEdge, exercisesEdge} {
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
