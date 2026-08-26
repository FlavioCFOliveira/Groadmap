package commands

// Regression tests for rmp task #288: a reverse-traversal READ reported the
// wrong edge when a node pair carried edges in both directions.
//
// The defect is upstream in GoGraph and cannot be fixed from this repository.
// The engine recovers a bound relationship's type and endpoints by probing the
// stored topology (`if !HasEdge(src,dst) && HasEdge(dst,src) then swap`). On a
// pair carrying edges BOTH ways the first conjunct is false, no swap fires, and
// the reverse leg of the traversal is hydrated from the FORWARD pair. Correcting
// the value in Groadmap's own result assembly was measured and is impossible:
// for `type(e)` and `startNode(e).key` the consumer receives a bare string with
// no relationship identity, and a `WHERE type(e) = …` row is dropped inside the
// engine before Groadmap sees anything. Groadmap's resolution is therefore to
// REFUSE the read rather than to answer it with a mislabelled edge
// (SPEC/GRAPH.md § Relationship Read Direction).
//
// Every fixture here is a BIDIRECTIONAL pair, because that is the only shape
// that misresolves: the same queries against a one-way pair answered correctly
// before the fix and would keep answering correctly after it, so a one-way
// fixture could not tell the two apart.
//
// These tests exercise the real command handlers against a real graph store, so
// they fail if either half of the contract breaks: if the refusal stops firing
// (a mislabelled edge is returned again), or if the refusal grows over-broad and
// the outgoing, anonymous, path or variable-length forms that DO resolve
// correctly stop being admitted.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	relReadSpecKey = "graph-read-direction"
	relReadTestKey = "graph_relread_test.go"
)

// seedBidirectionalPair creates the fixture every case below shares: a Spec node
// and a Test node carrying one edge EACH WAY — VERIFIED_BY running spec -> test
// and COVERS running test -> spec — plus a one-way IMPLEMENTED_BY edge to a Code
// node.
//
// The two types are deliberately DIFFERENT. A pair whose two edges share a type
// hides the defect's most visible symptom, because the forward type the engine
// substitutes happens to equal the reverse type it should have reported. The
// one-way edge is present so a test can show the very same traversal shape
// resolving correctly when the pair carries only one edge.
func seedBidirectionalPair(t *testing.T, roadmap string) {
	t.Helper()
	for _, q := range []string{
		"CREATE (:Spec {key:'" + relReadSpecKey + "'}), (:Test {key:'" + relReadTestKey + "'}), " +
			"(:Code {path:'internal/cypherguard/relread.go'})",
		"MATCH (s:Spec {key:'" + relReadSpecKey + "'}), (v:Test {key:'" + relReadTestKey + "'}) " +
			"MERGE (s)-[:VERIFIED_BY]->(v)",
		"MATCH (s:Spec {key:'" + relReadSpecKey + "'}), (v:Test {key:'" + relReadTestKey + "'}) " +
			"MERGE (v)-[:COVERS]->(s)",
		"MATCH (s:Spec {key:'" + relReadSpecKey + "'}), (c:Code {path:'internal/cypherguard/relread.go'}) " +
			"MERGE (s)-[:IMPLEMENTED_BY]->(c)",
	} {
		if err := runGraphCreate([]string{"-r", roadmap, "--query", q}); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

// graphQueryRows runs a read and returns its rows, failing the test if the read
// is refused or the output is not the columns/rows shape.
func graphQueryRows(t *testing.T, roadmap, query string) [][]any {
	t.Helper()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphQuery([]string{"-r", roadmap, "--query", query}); err != nil {
			t.Fatalf("read was refused: %v\nquery=%s", err, query)
		}
	})
	var parsed struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("stdout is not the columns/rows shape: %v\nstdout=%q", err, stdout)
	}
	return parsed.Rows
}

// TestGraphRead_RelationshipReadDirection is the core regression: the same
// relationship, reached by the three pattern orientations, on a pair that
// carries edges both ways. Only the outgoing form may be answered.
func TestGraphRead_RelationshipReadDirection(t *testing.T) {
	const roadmap = "graph-relread-core"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedBidirectionalPair(t, roadmap)

	t.Run("an undirected read is refused", func(t *testing.T) {
		err := runGraphQuery([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e]-(x) RETURN type(e), x.key"})
		if !errors.Is(err, utils.ErrValidation) {
			t.Fatalf("an undirected read must be refused as a validation error, got %v", err)
		}
		for _, want := range []string{"cannot read relationship", `"e"`, "undirected pattern"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal must name %q; got %v", want, err)
			}
		}
	})

	t.Run("an incoming read is refused", func(t *testing.T) {
		err := runGraphQuery([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'" + relReadSpecKey + "'})<-[e]-(x) RETURN type(e)"})
		if !errors.Is(err, utils.ErrValidation) {
			t.Fatalf("an incoming read must be refused as a validation error, got %v", err)
		}
		if !strings.Contains(err.Error(), "incoming pattern") {
			t.Errorf("refusal must name the incoming pattern; got %v", err)
		}
	})

	t.Run("an incoming read narrowed by a type filter is refused too", func(t *testing.T) {
		// The filter SELECTS the right edge — matching is not the broken part —
		// and the engine then reports it under the other edge's type. That is
		// the recorded symptom in its sharpest form, so the type filter must not
		// be read as making the traversal safe.
		err := runGraphQuery([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'" + relReadSpecKey + "'})<-[e:COVERS]-(x) RETURN type(e)"})
		if !errors.Is(err, utils.ErrValidation) {
			t.Fatalf("a type-filtered incoming read must be refused, got %v", err)
		}
	})

	t.Run("the outgoing read is answered and reports the true type", func(t *testing.T) {
		rows := graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[e:VERIFIED_BY]->(x) RETURN type(e), x.key")
		if len(rows) != 1 || rows[0][0] != "VERIFIED_BY" || rows[0][1] != relReadTestKey {
			t.Fatalf("outgoing read did not report the stored edge: %v", rows)
		}
	})

	t.Run("the outgoing rewrite reaches the reverse edge, so the refusal costs no reach", func(t *testing.T) {
		// This is the rewrite the refusal names: anchor the OUTGOING pattern on
		// the node the edge arrives at, instead of reversing the arrow. It must
		// report COVERS — the very relationship the refused incoming read
		// mislabelled as VERIFIED_BY — with the stored orientation intact.
		rows := graphQueryRows(t, roadmap,
			"MATCH (x)-[e]->(s:Spec {key:'"+relReadSpecKey+"'}) "+
				"RETURN type(e), startNode(e).key, endNode(e).key")
		if len(rows) != 1 {
			t.Fatalf("expected exactly one edge arriving at the Spec node, got %v", rows)
		}
		if rows[0][0] != "COVERS" {
			t.Errorf("outgoing rewrite reported type %v, want COVERS", rows[0][0])
		}
		if rows[0][1] != relReadTestKey || rows[0][2] != relReadSpecKey {
			t.Errorf("outgoing rewrite reported orientation %v -> %v, want %s -> %s",
				rows[0][1], rows[0][2], relReadTestKey, relReadSpecKey)
		}
	})

	t.Run("the union of the two outgoing legs recovers the whole undirected read", func(t *testing.T) {
		rows := graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[e]->(x) RETURN type(e) AS t, x.key AS k "+
				"UNION ALL "+
				"MATCH (x)-[e]->(s:Spec {key:'"+relReadSpecKey+"'}) RETURN type(e) AS t, x.key AS k")
		got := make(map[string]string, len(rows))
		for _, r := range rows {
			typ, _ := r[0].(string)
			key, _ := r[1].(string)
			got[typ] = key
		}
		for typ, key := range map[string]string{
			"VERIFIED_BY":    relReadTestKey,
			"COVERS":         relReadTestKey,
			"IMPLEMENTED_BY": "",
		} {
			if key == "" {
				if _, ok := got[typ]; !ok {
					t.Errorf("union rewrite lost the %s edge: %v", typ, rows)
				}
				continue
			}
			if got[typ] != key {
				t.Errorf("union rewrite reported %s -> %q, want %q: %v", typ, got[typ], key, rows)
			}
		}
	})
}

// TestGraphRead_EveryExpressionUseIsRefused pins the breadth of the rule. The
// refusal is keyed on READING the relationship's value, not on projecting it:
// the WHERE form never reaches the caller at all (the engine drops the row
// against the corrupted type), so a rule that guarded projections alone would
// leave the most silent symptom in place.
func TestGraphRead_EveryExpressionUseIsRefused(t *testing.T) {
	const roadmap = "graph-relread-uses"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedBidirectionalPair(t, roadmap)

	anchor := "MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e]-(x) "
	for name, query := range map[string]string{
		"projected whole value":    anchor + "RETURN e",
		"projected type":           anchor + "RETURN type(e)",
		"projected endpoints":      anchor + "RETURN startNode(e).key, endNode(e).key",
		"projected property":       anchor + "RETURN e.last_commit",
		"star projection":          anchor + "RETURN *",
		"where predicate":          anchor + "WHERE type(e) = 'COVERS' RETURN x.key",
		"order by":                 anchor + "RETURN x.key ORDER BY type(e)",
		"carried through with":     anchor + "WITH type(e) AS t RETURN t",
		"function over properties": anchor + "RETURN keys(e)",
	} {
		t.Run(name, func(t *testing.T) {
			if err := runGraphQuery([]string{"-r", roadmap, "--query", query}); !errors.Is(err, utils.ErrValidation) {
				t.Fatalf("%s must be refused as a validation error, got %v\nquery=%s", name, err, query)
			}
		})
	}

	t.Run("graph search is refused on the same shape", func(t *testing.T) {
		if err := runGraphSearch([]string{"-r", roadmap, "--query", anchor + "RETURN type(e)"}); !errors.Is(err, utils.ErrValidation) {
			t.Fatalf("graph search must apply the same rule, got %v", err)
		}
	})
}

// TestGraphRead_RefusalDoesNotSpread pins the shapes that resolve correctly and
// must keep being admitted. Each one was measured against a bidirectional pair
// and reports the true stored type and orientation, so refusing it would cost
// reach for nothing.
func TestGraphRead_RefusalDoesNotSpread(t *testing.T) {
	const roadmap = "graph-relread-scope"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedBidirectionalPair(t, roadmap)

	t.Run("an anonymous undirected relationship is admitted", func(t *testing.T) {
		// No variable is bound, so no relationship value is ever built and
		// nothing can be misreported.
		rows := graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[:COVERS]-(x) RETURN x.key")
		if len(rows) != 1 || rows[0][0] != relReadTestKey {
			t.Fatalf("anonymous undirected read did not reach the Test node: %v", rows)
		}
	})

	t.Run("a named path over an undirected pattern is admitted and correct", func(t *testing.T) {
		// The path renderer uses the resolver that is TOLD the traversal
		// direction, so it reports both edges with their true types.
		stdout, _ := captureStdStreams(t, func() {
			if err := runGraphQuery([]string{"-r", roadmap, "--query",
				"MATCH p=(s:Spec {key:'" + relReadSpecKey + "'})-[e]-(v:Test) RETURN p"}); err != nil {
				t.Fatalf("named-path read was refused: %v", err)
			}
		})
		for _, want := range []string{"VERIFIED_BY", "COVERS"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("named-path read lost the %s edge:\n%s", want, stdout)
			}
		}
	})

	t.Run("a variable-length undirected relationship is admitted and correct", func(t *testing.T) {
		stdout, _ := captureStdStreams(t, func() {
			if err := runGraphSearch([]string{"-r", roadmap, "--query",
				"MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e*1..1]-(v:Test) RETURN e"}); err != nil {
				t.Fatalf("variable-length read was refused: %v", err)
			}
		})
		for _, want := range []string{"VERIFIED_BY", "COVERS"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("variable-length read lost the %s edge:\n%s", want, stdout)
			}
		}
	})

	t.Run("a node write reached through an undirected traversal is admitted", func(t *testing.T) {
		// The relationship is bound but never read, and the write targets a
		// node, which is resolved by identifier rather than by endpoint pair.
		if err := runGraphUpdate([]string{"-r", roadmap, "--query",
			"MATCH (v:Test {key:'" + relReadTestKey + "'})-[e]-(x) SET x.reviewed = true"}); err != nil {
			t.Fatalf("a node write through an undirected traversal must stay accepted: %v", err)
		}
	})

	t.Run("a delete through an undirected traversal is admitted", func(t *testing.T) {
		// DELETE resolves the edge itself rather than through the endpoint
		// columns, so it removes the right one.
		if err := runGraphDelete([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e:COVERS]-(v:Test) DELETE e"}); err != nil {
			t.Fatalf("an undirected delete must stay accepted: %v", err)
		}
		rows := graphQueryRows(t, roadmap,
			"MATCH (v:Test {key:'"+relReadTestKey+"'})-[e:COVERS]->(s) RETURN type(e)")
		if len(rows) != 0 {
			t.Errorf("the undirected delete reported success but the edge survived: %v", rows)
		}
	})
}

// TestGraphUpdate_MisreadRelationshipIsNotPersisted covers the consequence that
// carried the decision: the corrupted value does not stay inside the read path.
// `SET <node>.p = type(e)` through an incoming pattern exited 0, reported
// {"ok": true}, and PERSISTED the forward edge's type onto the node — writing
// wrong data to disk while reporting success. The assertion is on storage, not
// on the exit code: a refusal that still wrote the property would pass an
// exit-code-only test.
func TestGraphUpdate_MisreadRelationshipIsNotPersisted(t *testing.T) {
	const roadmap = "graph-relread-writeleak"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedBidirectionalPair(t, roadmap)

	err := runGraphUpdate([]string{"-r", roadmap, "--query",
		"MATCH (s:Spec {key:'" + relReadSpecKey + "'})<-[e]-(v:Test) SET v.last_edge_type = type(e)"})
	// Reported with Errorf, not Fatalf, so the storage assertion below still
	// runs when the refusal is absent. The exit code is the cheap half of this
	// contract; that nothing reached the disk is the half that matters, and it
	// has to be able to fail on its own.
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("a node write deriving its VALUE from an incoming-bound relationship must be "+
			"refused as a validation error, got %v", err)
	} else if !strings.Contains(err.Error(), "cannot read relationship") {
		t.Errorf("the refusal must come from the READ rule, which owns the right-hand side; got %v", err)
	}

	rows := graphQueryRows(t, roadmap,
		"MATCH (v:Test {key:'"+relReadTestKey+"'}) RETURN v.last_edge_type")
	if len(rows) != 1 {
		t.Fatalf("expected exactly one Test node, got %v", rows)
	}
	if rows[0][0] != nil {
		t.Fatalf("the refused write still reached storage: v.last_edge_type = %v "+
			"(before the fix this persisted %q, the FORWARD edge's type, where the truth is %q)",
			rows[0][0], "VERIFIED_BY", "COVERS")
	}
}

// TestGraphDelete_PredicateGatedDeleteIsRefusedAndRemovesNothing covers the
// sharpest consequence in this family, and the reason the DELETE exemption is
// drawn by CLAUSE rather than by command.
//
// A bare `DELETE e` is correct: the engine resolves that edge itself rather than
// through the endpoint columns. But the moment a predicate over `type(e)`
// decides WHICH edges are deleted, the engine evaluates the corrupted type,
// drops the row, and a DESTRUCTIVE statement exits 0 reporting `{"ok": true}`
// having removed nothing at all. The caller has no reason to check, which is
// what makes it worse than a mislabelled read.
//
// Both assertions are on storage. An exit-code-only test would pass against an
// implementation that refused the statement and deleted the edges anyway, and —
// far more likely — against one that accepted it and deleted nothing.
func TestGraphDelete_PredicateGatedDeleteIsRefusedAndRemovesNothing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		dir     string
	}{
		{"undirected", "(s:Spec {key:'" + relReadSpecKey + "'})-[e]-(v:Test)", "undirected pattern"},
		{"incoming", "(s:Spec {key:'" + relReadSpecKey + "'})<-[e]-(v:Test)", "incoming pattern"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roadmap := "graph-relread-del-" + tc.name
			defer setupTestGraphRoadmap(t, roadmap)()
			seedBidirectionalPair(t, roadmap)

			err := runGraphDelete([]string{"-r", roadmap, "--query",
				"MATCH " + tc.pattern + " WHERE type(e) = 'COVERS' DELETE e"})
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("a DELETE gated by a predicate over a misresolved relationship must be "+
					"refused as a validation error, got %v", err)
			} else if !strings.Contains(err.Error(), tc.dir) {
				t.Errorf("the refusal must name the %s; got %v", tc.dir, err)
			}

			// Storage is the real assertion: BOTH edges must survive. Before the
			// fix this exited 0 with {"ok": true} and also left both in place —
			// so the exit code alone cannot tell the two apart, but the pairing
			// of a refusal with intact storage can.
			rows := graphQueryRows(t, roadmap,
				"MATCH (a)-[e]->(b) WHERE a.key IN ['"+relReadSpecKey+"','"+relReadTestKey+"'] "+
					"AND b.key IN ['"+relReadSpecKey+"','"+relReadTestKey+"'] RETURN type(e)")
			got := make(map[string]bool, len(rows))
			for _, r := range rows {
				typ, _ := r[0].(string)
				got[typ] = true
			}
			if !got["VERIFIED_BY"] || !got["COVERS"] {
				t.Fatalf("a refused DELETE must leave both edges in place; surviving types = %v", got)
			}
		})
	}
}

// TestGraphDelete_BareDeleteThroughAReversePatternStillWorks is the other half,
// and it matters as much as the refusals: it is what stops the fix over-reaching
// into the one DELETE shape that was always correct.
func TestGraphDelete_BareDeleteThroughAReversePatternStillWorks(t *testing.T) {
	const roadmap = "graph-relread-del-bare"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedBidirectionalPair(t, roadmap)

	if err := runGraphDelete([]string{"-r", roadmap, "--query",
		"MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e:COVERS]-(v:Test) DELETE e"}); err != nil {
		t.Fatalf("a bare DELETE through an undirected pattern must stay accepted: %v", err)
	}

	// It removed the right edge...
	gone := graphQueryRows(t, roadmap,
		"MATCH (v:Test {key:'"+relReadTestKey+"'})-[e:COVERS]->(s) RETURN type(e)")
	if len(gone) != 0 {
		t.Errorf("the bare DELETE reported success but the COVERS edge survived: %v", gone)
	}
	// ...and only that one.
	kept := graphQueryRows(t, roadmap,
		"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[e:VERIFIED_BY]->(v) RETURN type(e)")
	if len(kept) != 1 {
		t.Errorf("the bare DELETE removed the wrong edge: VERIFIED_BY rows = %v", kept)
	}
}
