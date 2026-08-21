package commands

// Regression tests for rmp task #193: `rmp graph update` reported success for a
// SET on a relationship matched by an undirected pattern while writing nothing.
//
// The defect is upstream in GoGraph and cannot be fixed from this repository:
// the engine emits a relationship's endpoint columns in PATTERN order, its READ
// path corrects that orientation against the stored edge and its WRITE path does
// not, and the storage layer answers a write to a pair that has no edge with a
// documented no-op. Groadmap's resolution is therefore to REFUSE the query
// rather than to execute it and report unqualified success
// (SPEC/GRAPH.md § Relationship Write Direction).
//
// These tests exercise the real command handlers against a real graph store, so
// they fail if either half of the contract breaks: if the refusal stops firing
// (the silent no-op returns), or if the refusal grows over-broad and the
// outgoing forms that DO write stop being admitted.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// seedProvenanceEdge creates the two-node, one-edge fixture the direction tests
// share: a Spec node, a Test node, and a single VERIFIED_BY edge running
// spec -> test. Every case below writes to that one edge, so a property that
// reads back proves the write reached storage rather than a phantom pair.
func seedProvenanceEdge(t *testing.T, roadmap string) {
	t.Helper()
	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (:Spec {key:'graph-write-direction'}), (:Test {key:'graph_relwrite_test.go'})"}); err != nil {
		t.Fatalf("seed nodes: %v", err)
	}
	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"MATCH (s:Spec {key:'graph-write-direction'}), (v:Test {key:'graph_relwrite_test.go'}) " +
			"MERGE (s)-[:VERIFIED_BY]->(v)"}); err != nil {
		t.Fatalf("seed edge: %v", err)
	}
}

// readEdgeProperty returns the value of key on the seeded edge, read through the
// directed pattern that matches how it is stored, and reports whether the
// property is present (a JSON null reads as absent).
//
// The read-back is deliberately NOT expressed with the undirected pattern the
// write side refuses: the point is to observe storage, and an undirected read is
// the very thing whose agreement with the write is in question.
func readEdgeProperty(t *testing.T, roadmap, key string) (string, bool) {
	t.Helper()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphQuery([]string{"-r", roadmap, "--query",
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

// TestGraphUpdate_RelationshipWriteDirection is the core regression: the same
// edge, written from its SOURCE node and from its TARGET node, must never end in
// the state that defined the defect — a command that reported success while the
// property stayed absent.
func TestGraphUpdate_RelationshipWriteDirection(t *testing.T) {
	const roadmap = "graph-relwrite-direction"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedProvenanceEdge(t, roadmap)

	// ── Source node, outgoing: the write the engine honours ──────────────────
	t.Run("from the source node, outgoing, writes and reads back", func(t *testing.T) {
		err := runGraphUpdate([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'graph-write-direction'})-[e:VERIFIED_BY]->(v) " +
				"SET e.from_source = 'commit-aaa111'"})
		if err != nil {
			t.Fatalf("outgoing update from the source was refused: %v", err)
		}
		got, present := readEdgeProperty(t, roadmap, "from_source")
		if !present {
			t.Fatal("outgoing update from the source reported success but wrote nothing")
		}
		if got != "commit-aaa111" {
			t.Errorf("from_source = %q, want %q", got, "commit-aaa111")
		}
	})

	// ── Target node, outgoing: the documented repair, which must keep working ─
	t.Run("from the target node, outgoing, writes and reads back", func(t *testing.T) {
		err := runGraphUpdate([]string{"-r", roadmap, "--query",
			"MATCH (other)-[e:VERIFIED_BY]->(v:Test {key:'graph_relwrite_test.go'}) " +
				"SET e.from_target = 'commit-bbb222'"})
		if err != nil {
			t.Fatalf("outgoing update anchored on the target was refused: %v", err)
		}
		got, present := readEdgeProperty(t, roadmap, "from_target")
		if !present {
			t.Fatal("outgoing update anchored on the target reported success but wrote nothing: " +
				"the refusal of the reverse forms would then leave incoming edges unreachable")
		}
		if got != "commit-bbb222" {
			t.Errorf("from_target = %q, want %q", got, "commit-bbb222")
		}
	})

	// ── Target node, undirected: refused, and nothing written ────────────────
	t.Run("from the target node, undirected, is refused and writes nothing", func(t *testing.T) {
		err := runGraphUpdate([]string{"-r", roadmap, "--query",
			"MATCH (v:Test {key:'graph_relwrite_test.go'})-[e]-(x) " +
				"SET e.undirected = 'commit-ccc333'"})
		if err == nil {
			t.Fatal("undirected update from the target was accepted; it writes nothing and must be refused")
		}
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("error = %v, want it to wrap utils.ErrValidation (exit 6)", err)
		}
		if _, present := readEdgeProperty(t, roadmap, "undirected"); present {
			t.Error("a refused update reached the store")
		}
	})

	// ── Target node, incoming: refused, and nothing written ──────────────────
	t.Run("from the target node, incoming, is refused and writes nothing", func(t *testing.T) {
		err := runGraphUpdate([]string{"-r", roadmap, "--query",
			"MATCH (v:Test {key:'graph_relwrite_test.go'})<-[e:VERIFIED_BY]-(s) " +
				"SET e.incoming = 'commit-ddd444'"})
		if err == nil {
			t.Fatal("incoming update from the target was accepted; it writes nothing and must be refused")
		}
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("error = %v, want it to wrap utils.ErrValidation (exit 6)", err)
		}
		if _, present := readEdgeProperty(t, roadmap, "incoming"); present {
			t.Error("a refused update reached the store")
		}
	})

	// ── Source node, undirected: refused too ─────────────────────────────────
	//
	// This one WOULD have written, because every row it matches happens to run
	// forwards. It is refused all the same: whether an undirected pattern writes
	// depends on the data it meets, not on the query, so admitting the shape
	// would make the guarantee conditional on the store's contents.
	t.Run("from the source node, undirected, is refused although it would have written", func(t *testing.T) {
		err := runGraphUpdate([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'graph-write-direction'})-[e]-(x) " +
				"SET e.undirected_from_source = 'commit-eee555'"})
		if err == nil {
			t.Fatal("undirected update from the source was accepted; the shape's outcome is data-dependent and must be refused")
		}
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("error = %v, want it to wrap utils.ErrValidation (exit 6)", err)
		}
		if _, present := readEdgeProperty(t, roadmap, "undirected_from_source"); present {
			t.Error("a refused update reached the store")
		}
	})
}

// TestGraphUpdate_RefusalNamesTheUnmatchedDirection covers the acceptance
// criterion on the message itself: a refusal must name the relationship, the
// direction that would have been skipped, and the outgoing rewrite, so the
// operator can act on it without consulting the specification.
func TestGraphUpdate_RefusalNamesTheUnmatchedDirection(t *testing.T) {
	const roadmap = "graph-relwrite-message"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedProvenanceEdge(t, roadmap)

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name: "undirected",
			query: "MATCH (v:Test {key:'graph_relwrite_test.go'})-[e]-(x) " +
				"SET e.last_commit = 'commit-fff666'",
			want: []string{`"e"`, "undirected", "incoming", "outgoing"},
		},
		{
			name: "incoming",
			query: "MATCH (v:Test {key:'graph_relwrite_test.go'})<-[e:VERIFIED_BY]-(s) " +
				"SET e.last_commit = 'commit-fff666'",
			want: []string{`"e"`, "incoming", "outgoing"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runGraphUpdate([]string{"-r", roadmap, "--query", tc.query})
			if err == nil {
				t.Fatal("expected a refusal")
			}
			msg := err.Error()
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal message does not mention %q:\n%s", want, msg)
				}
			}
			// The message must show the repair, not merely forbid the query.
			if !strings.Contains(msg, "-[e]->") {
				t.Errorf("refusal message does not show the outgoing rewrite:\n%s", msg)
			}
		})
	}
}

// TestGraphUpdate_DirectionCheckLeavesOtherSubcommandsAlone pins the scope of the
// refusal. It is a write-path rule for `update` alone: `delete` removes a
// relationship bound by a reverse traversal correctly (the engine resolves the
// edge itself rather than through the endpoint columns), and the read
// subcommands must keep answering undirected patterns, which is precisely the
// half of the defect that always worked.
func TestGraphUpdate_DirectionCheckLeavesOtherSubcommandsAlone(t *testing.T) {
	const roadmap = "graph-relwrite-scope"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedProvenanceEdge(t, roadmap)

	t.Run("an undirected read is still answered", func(t *testing.T) {
		stdout, _ := captureStdStreams(t, func() {
			if err := runGraphQuery([]string{"-r", roadmap, "--query",
				"MATCH (v:Test {key:'graph_relwrite_test.go'})-[e]-(x) RETURN type(e), x.key"}); err != nil {
				t.Fatalf("undirected read was refused: %v", err)
			}
		})
		if !strings.Contains(stdout, "VERIFIED_BY") {
			t.Errorf("undirected read did not report the incoming edge:\n%s", stdout)
		}
	})

	t.Run("an undirected delete is still accepted", func(t *testing.T) {
		if err := runGraphDelete([]string{"-r", roadmap, "--query",
			"MATCH (v:Test {key:'graph_relwrite_test.go'})-[e]-(x) DELETE e"}); err != nil {
			t.Fatalf("undirected delete was refused: %v", err)
		}
		stdout, _ := captureStdStreams(t, func() {
			if err := runGraphQuery([]string{"-r", roadmap, "--query",
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
	})
}
