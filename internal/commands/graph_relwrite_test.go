package commands

// Tests for the relationship-write hazard published as SPEC/GRAPH.md § What
// Groadmap Does Not Check, item 4, and asserted by acceptance criterion 38.
//
// The engine writes a relationship property by its endpoint PAIR, and it takes
// that pair from the columns the expansion emitted. Those columns carry the
// relationship the way the pattern walked it, not the way storage holds it, so a
// relationship reached against the stored arrow is addressed as a pair that has
// no relationship. The write is not refused; it is MISFILED. One store answers a
// write to an absent pair with a documented no-op, but the other does not test
// the pair at all: it records the property under the correct handle in a bucket
// keyed by the reversed pair, which no read consults. Nothing is readable, no
// error is raised, and the transaction still commits.
//
// How much of a statement's write survives is decided by the DATA rather than by
// the statement: a write persists only where the row's left-bound node is the
// stored source and its right-bound node the stored target, so the same
// statement may write every relationship it matched, some of them, or none.
// The tests below fix the stored orientation of every relationship they measure,
// because the shape of a statement does not tell an assertion what to expect.
//
// Groadmap used to REFUSE such a statement. It no longer does: it hands the
// engine whatever it is given and holds no opinion about the patterns a
// statement binds. The tests below therefore assert the hazard as the specified
// OUTCOME — the statement succeeds and the property is absent — rather than the
// refusal that used to stand in front of it. That direction is deliberate: an
// absence of checking cannot be tested, and an outcome can, so a check
// reintroduced here fails these tests instead of passing them.
//
// The other half is the reach, which the hazard does not cost: EVERY
// relationship is writable through an outgoing pattern, because an outgoing
// pattern may be anchored on either endpoint. Those cases are asserted too, so a
// regression in the write path that broke them could not hide behind the hazard.

import (
	"encoding/json"
	"strings"
	"testing"
)

// seedProvenanceEdge creates the two-node, one-edge fixture the direction tests
// share: a Spec node, a Test node, and a single VERIFIED_BY edge running
// spec -> test. Every case below writes to that one edge, so a property that
// reads back proves the write reached storage rather than a phantom pair.
func seedProvenanceEdge(t *testing.T, roadmap string) {
	t.Helper()
	if err := runGraphExecute([]string{"-r", roadmap, "--query",
		"CREATE (:Spec {key:'graph-write-direction'}), (:Test {key:'graph_relwrite_test.go'})"}); err != nil {
		t.Fatalf("seed nodes: %v", err)
	}
	if err := runGraphExecute([]string{"-r", roadmap, "--query",
		"MATCH (s:Spec {key:'graph-write-direction'}), (v:Test {key:'graph_relwrite_test.go'}) " +
			"MERGE (s)-[:VERIFIED_BY]->(v)"}); err != nil {
		t.Fatalf("seed edge: %v", err)
	}
}

// readEdgeProperty returns the value of key on the seeded edge, read through the
// directed pattern that matches how it is stored, and reports whether the
// property is present (a JSON null reads as absent).
//
// The read-back is deliberately expressed with the OUTGOING pattern, which
// SPEC/GRAPH.md § What Groadmap Does Not Check item 5 states is correct whatever
// the data: the point is to observe storage, and an undirected read is the very
// thing whose agreement with the write is in question.
func readEdgeProperty(t *testing.T, roadmap, key string) (string, bool) {
	t.Helper()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'graph-write-direction'})-[e:VERIFIED_BY]->(v) RETURN e." + key}); err != nil {
			t.Fatalf("read back %q: %v", key, err)
		}
	})

	var parsed struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("read back %q: stdout is not the columns/rows shape: %v\nstdout=%q", key, err, stdout)
	}
	if len(parsed.Rows) != 1 {
		t.Fatalf("read back %q: expected exactly one edge row, got %d\nstdout=%q", key, len(parsed.Rows), stdout)
	}
	if parsed.Rows[0][0] == nil {
		return "", false
	}
	s, ok := parsed.Rows[0][0].(string)
	if !ok {
		t.Fatalf("read back %q: value is %T, want string", key, parsed.Rows[0][0])
	}
	return s, true
}

// TestGraphUpdate_RelationshipWriteDirection is the core case: the same edge,
// written from its SOURCE node and from its TARGET node, through each pattern
// orientation. The outgoing forms write and read back; the forms that reach the
// edge against the stored arrow report success and write nothing.
func TestGraphUpdate_RelationshipWriteDirection(t *testing.T) {
	const roadmap = "graph-relwrite-direction"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedProvenanceEdge(t, roadmap)

	// -- Source node, outgoing: the write the engine honours --------------------
	t.Run("from the source node, outgoing, writes and reads back", func(t *testing.T) {
		err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'graph-write-direction'})-[e:VERIFIED_BY]->(v) " +
				"SET e.from_source = 'commit-aaa111'"})
		if err != nil {
			t.Fatalf("outgoing update from the source failed: %v", err)
		}
		got, present := readEdgeProperty(t, roadmap, "from_source")
		if !present {
			t.Fatal("outgoing update from the source reported success but wrote nothing")
		}
		if got != "commit-aaa111" {
			t.Errorf("from_source = %q, want %q", got, "commit-aaa111")
		}
	})

	// -- Target node, outgoing: the whole of the reach, anchored the other way --
	t.Run("from the target node, outgoing, writes and reads back", func(t *testing.T) {
		err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (other)-[e:VERIFIED_BY]->(v:Test {key:'graph_relwrite_test.go'}) " +
				"SET e.from_target = 'commit-bbb222'"})
		if err != nil {
			t.Fatalf("outgoing update anchored on the target failed: %v", err)
		}
		got, present := readEdgeProperty(t, roadmap, "from_target")
		if !present {
			t.Fatal("outgoing update anchored on the target reported success but wrote nothing: " +
				"an outgoing pattern must be able to reach the edges ARRIVING at a node, " +
				"or the hazard below would cost reach as well as silence")
		}
		if got != "commit-bbb222" {
			t.Errorf("from_target = %q, want %q", got, "commit-bbb222")
		}
	})

	// -- Target node, incoming: acceptance criterion 38's third bullet ----------
	t.Run("from the target node, incoming, reports success and writes nothing", func(t *testing.T) {
		err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (v:Test {key:'graph_relwrite_test.go'})<-[e:VERIFIED_BY]-(s) " +
				"SET e.last_commit = 'commit-ddd444'"})
		if err != nil {
			t.Fatalf("the statement must EXECUTE: Groadmap no longer inspects the patterns "+
				"a statement binds (SPEC/GRAPH.md § What Groadmap Does Not Check, item 4); got %v", err)
		}
		if got, present := readEdgeProperty(t, roadmap, "last_commit"); present {
			t.Fatalf("last_commit = %q: the incoming leg is expected to write NOTHING. "+
				"If the engine has learned to write it, item 4 of "+
				"SPEC/GRAPH.md § What Groadmap Does Not Check is stale and must be corrected", got)
		}
	})

	// -- Target node, undirected: the half of the traversal that runs backwards -
	t.Run("from the target node, undirected, reports success and writes nothing", func(t *testing.T) {
		err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (v:Test {key:'graph_relwrite_test.go'})-[e]-(x) " +
				"SET e.undirected = 'commit-ccc333'"})
		if err != nil {
			t.Fatalf("the statement must execute; got %v", err)
		}
		if got, present := readEdgeProperty(t, roadmap, "undirected"); present {
			t.Fatalf("undirected = %q: every row this pattern matches runs against the "+
				"stored arrow, so the write is expected to be dropped", got)
		}
	})

	// -- Source node, undirected: the same shape, and it DOES write -------------
	//
	// This is what makes the hazard a hazard rather than a rule: the outcome of
	// an undirected pattern depends on the data it meets and not on the query.
	// The two undirected subtests differ only in which endpoint they anchor on,
	// and they end in opposite states.
	t.Run("from the source node, undirected, writes because every matched row runs forwards", func(t *testing.T) {
		err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'graph-write-direction'})-[e]-(x) " +
				"SET e.undirected_from_source = 'commit-eee555'"})
		if err != nil {
			t.Fatalf("the statement must execute; got %v", err)
		}
		got, present := readEdgeProperty(t, roadmap, "undirected_from_source")
		if !present {
			t.Fatal("undirected_from_source is absent: anchored on the source, every row this " +
				"pattern matches runs WITH the stored arrow, so the write must land")
		}
		if got != "commit-eee555" {
			t.Errorf("undirected_from_source = %q, want %q", got, "commit-eee555")
		}
	})
}

// TestGraphDelete_UndirectedBareDeleteRemovesTheEdge pins the shape the hazard
// does NOT reach. A bare DELETE names the relationship as a delete target rather
// than as an expression, and the engine resolves that relationship itself rather
// than through the endpoint columns, so it removes the right one whichever way
// the pattern walked it (SPEC/GRAPH.md § What Groadmap Does Not Check, item 5).
func TestGraphDelete_UndirectedBareDeleteRemovesTheEdge(t *testing.T) {
	const roadmap = "graph-relwrite-scope"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedProvenanceEdge(t, roadmap)

	if err := runGraphExecute([]string{"-r", roadmap, "--query",
		"MATCH (v:Test {key:'graph_relwrite_test.go'})-[e]-(x) DELETE e"}); err != nil {
		t.Fatalf("undirected delete failed: %v", err)
	}
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'graph-write-direction'})-[e:VERIFIED_BY]->(v) RETURN e"}); err != nil {
			t.Fatalf("post-delete read: %v", err)
		}
	})
	var parsed struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("post-delete read is not the columns/rows shape: %v\nstdout=%q", err, stdout)
	}
	if len(parsed.Rows) != 0 {
		t.Errorf("undirected delete reported success but the edge survived: %s", stdout)
	}
}
