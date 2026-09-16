package commands

// Regression test for an entity-valued property write (SPEC/DATA_FORMATS.md
// § One Realisation of the Mapping, and § Graph element mapping, rule 1).
//
// A property value is never a node, a relationship or a path. The engine used to
// accept such a write on some forms, report success and store nothing, so a
// caller was told a property had been set that no later statement could read.
// Since GoGraph #2816 the engine refuses every such write with
// InvalidPropertyType, on every write form, on a node and on a relationship
// alike, and this file holds it to that:
//
//   - the invocation fails as an ordinary engine failure — exit code 1, the
//     parse-or-execution line, and nothing on stdout
//     (SPEC/GRAPH.md § Error Handling and Exit Codes, rule 2);
//   - the key it named is absent afterwards, and a replacing or merging form
//     leaves the properties the element already carried exactly as they were;
//   - a creating form creates nothing.
//
// The engine's diagnostic is not matched. The server does not forward it for this
// refusal, and its wording is the engine's in any case.
//
// Every statement that creates a relationship does so between two nodes that no
// relationship joins in that direction, because a failed statement that creates a
// relationship between nodes already joined in the same direction replaces one
// of the pre-existing relationships (SPEC/GRAPH.md § Error Handling and Exit
// Codes, rule 9), and that defect is not this test's subject.
//
// The whole graph is compared before and after every case, so a case that changed
// anything at all — the key, a sibling property, another element — fails, and not
// only a case that stored the key.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// entityValueSeeds is the fixture: three components, and one relationship
// between the first two. alert-router and pager-bridge are joined by nothing, and
// neither are ingest-gateway and pager-bridge.
var entityValueSeeds = []string{
	"CREATE (:Component {key:'ingest-gateway', language:'go', owner:'platform'}), " +
		"(:Component {key:'alert-router', language:'go'}), " +
		"(:Component {key:'pager-bridge', language:'rust'})",
	"MATCH (a:Component {key:'ingest-gateway'}), (b:Component {key:'alert-router'}) " +
		"CREATE (a)-[:CALLS {proto:'grpc', since:'2026-01'}]->(b)",
}

// graphSnapshot renders every node and every relationship of the graph, with
// their labels, types and properties, as one deterministic string.
func graphSnapshot(t *testing.T, roadmap string) string {
	t.Helper()
	nodes := graphQueryRows(t, roadmap,
		"MATCH (n) RETURN n.key AS key, labels(n) AS labels, properties(n) AS props ORDER BY key")
	relationships := graphQueryRows(t, roadmap,
		"MATCH (a)-[r]->(b) RETURN a.key AS source, type(r) AS type, properties(r) AS props, "+
			"b.key AS target ORDER BY source, target, type")
	rendered, err := json.Marshal(map[string][][]any{"nodes": nodes, "relationships": relationships})
	if err != nil {
		t.Fatalf("rendering the graph snapshot: %v", err)
	}
	return string(rendered)
}

// TestGraphWrite_AnEntityValuedPropertyIsRefusedAndWritesNothing drives every
// write form SPEC/DATA_FORMATS.md § One Realisation of the Mapping names, with a
// node, a relationship, a path, or a list holding a node as the value, on a node
// and on a relationship.
func TestGraphWrite_AnEntityValuedPropertyIsRefusedAndWritesNothing(t *testing.T) {
	const roadmap = "graph-entity-value"
	defer servedRoadmap(t, roadmap)()
	for _, seed := range entityValueSeeds {
		if err := runGraphClient([]string{"-r", roadmap, "--query", seed}); err != nil {
			t.Fatalf("seed %q: %v", seed, err)
		}
	}
	before := graphSnapshot(t, roadmap)

	const (
		gatewayPeer = "MATCH (a:Component {key:'ingest-gateway'}), (b:Component {key:'pager-bridge'}) "
		pager       = "MATCH (b:Component {key:'pager-bridge'}) "
		call        = "MATCH (a:Component {key:'ingest-gateway'})-[r:CALLS]->(x), " +
			"(c:Component {key:'pager-bridge'}) "
		unjoined = "MATCH (a:Component {key:'alert-router'}), (c:Component {key:'pager-bridge'}) "
	)
	cases := []struct {
		name      string
		statement string
	}{
		// On a node.
		{"a node assigned to a node property", gatewayPeer + "SET a.peer = b"},
		{"a relationship assigned to a node property",
			"MATCH (a:Component {key:'ingest-gateway'})-[r:CALLS]->() SET a.link = r"},
		{"a path assigned to a node property",
			"MATCH p = (a:Component {key:'ingest-gateway'})-[:CALLS]->() SET a.route = p"},
		{"a list holding a node assigned to a node property", gatewayPeer + "SET a.peers = [b]"},
		{"a replacing map carrying a node, on a node", gatewayPeer + "SET a = {peer: b, language: 'zig'}"},
		{"a merging map carrying a node, on a node", gatewayPeer + "SET a += {peer: b, language: 'zig'}"},
		{"ON MATCH SET of a node, on a node",
			pager + "MERGE (a:Component {key:'ingest-gateway'}) ON MATCH SET a.peer = b"},
		{"ON CREATE SET of a node, on a node",
			pager + "MERGE (n:Runbook {key:'pager-escalation'}) ON CREATE SET n.subject = b"},
		{"an inline node map in CREATE carrying a node",
			pager + "CREATE (:Runbook {key:'pager-handover', subject: b})"},
		{"an inline node map in MERGE carrying a node",
			pager + "MERGE (:Runbook {key:'pager-rotation', subject: b})"},

		// On a relationship.
		{"a node assigned to a relationship property", call + "SET r.peer = c"},
		{"a relationship assigned to a relationship property", call + "SET r.self = r"},
		{"a replacing map carrying a node, on a relationship", call + "SET r = {peer: c, proto: 'http'}"},
		{"a merging map carrying a node, on a relationship", call + "SET r += {peer: c, proto: 'http'}"},
		{"ON MATCH SET of a node, on a relationship",
			"MATCH (a:Component {key:'ingest-gateway'}), (x:Component {key:'alert-router'}), " +
				"(c:Component {key:'pager-bridge'}) MERGE (a)-[r:CALLS]->(x) ON MATCH SET r.peer = c"},
		{"ON CREATE SET of a node, on a relationship",
			unjoined + "MERGE (a)-[r:NOTIFIES]->(c) ON CREATE SET r.peer = c"},
		{"an inline relationship map in CREATE carrying a node",
			unjoined + "CREATE (a)-[:NOTIFIES {peer: c}]->(c)"},
		{"an inline relationship map in MERGE carrying a node",
			unjoined + "MERGE (a)-[:NOTIFIES {peer: c}]->(c)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			stdout, _ := captureStdStreams(t, func() {
				err = runGraphClient([]string{"-r", roadmap, "--query", tc.statement})
			})
			if err == nil {
				t.Fatalf("the write was accepted; a node, a relationship or a path is never a "+
					"property value and the engine refuses it (SPEC/DATA_FORMATS.md § One "+
					"Realisation of the Mapping)\nstatement: %s\nstdout: %q", tc.statement, stdout)
			}
			if !errors.Is(err, utils.ErrGraphEngine) {
				t.Errorf("err = %v, want it to wrap utils.ErrGraphEngine (exit code 1)", err)
			}
			if !strings.HasPrefix(err.Error(), "graph engine error: graph query failed: ") {
				t.Errorf("the refusal does not write the parse/execution line: %q", err.Error())
			}
			if stdout != "" {
				t.Errorf("a refused write printed to stdout: %q", stdout)
			}
			if after := graphSnapshot(t, roadmap); after != before {
				t.Fatalf("the refused write changed the graph\nstatement: %s\nbefore: %s\nafter:  %s",
					tc.statement, before, after)
			}
		})
	}
}
