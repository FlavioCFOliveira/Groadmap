package commands

// Tests for the relationship-identity behaviour recorded in SPEC/GRAPH.md § What
// Groadmap Does Not Check, item 8, for rule 9 of § Error Handling and Exit Codes,
// and for acceptance criterion 87, asserted in both directions as mitigation 3 of
// § Dependency Maturity Risk requires.
//
// Item 8 has two conditions, and how the binding was made decides which applies.
//
// A `MERGE` whose relationship pattern joins two nodes the statement has already
// bound, over a pair that several relationships join in the pattern's direction,
// matches EVERY one of them and emits one row per relationship, each binding a
// different one: a write, a removal and a DELETE through the binding reach every
// matched relationship and no other, and a MERGE whose inline property map one of
// them satisfies creates nothing.
//
// A relationship bound by `CREATE`, or by a `MERGE` that created it, takes every
// assignment, removal and deletion on itself, on a pair a relationship already
// joins in that direction as on any other, and the pair's pre-existing
// relationships keep every property they held.
//
// An earlier pinned engine got both wrong, silently — one relationship of the
// pair took every row of a matching MERGE, and a created binding's DELETE removed
// a pre-existing relationship instead — and rule 9 of § Error Handling and Exit
// Codes records the third: a failed statement that had created a relationship on
// a joined pair left one of the pair's relationships replaced. Each is a boundary
// an engine change can move back, so each is asserted here on the read-back and
// not on the exit code.
//
// Every read-back uses an outgoing MATCH, which SPEC/GRAPH.md § What Groadmap
// Does Not Check, item 5, states is resolved correctly whatever the data.

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// mergeBindingStale is the reason every failure below names.
const mergeBindingStale = "the engine's behaviour has moved and SPEC/GRAPH.md § What Groadmap Does Not " +
	"Check, item 8, and acceptance criterion 87 no longer describe it; the item and mitigation 3 must be corrected"

// seedParallelPair creates a Spec node and a Test node joined, spec -> test, by a
// VERIFIED_BY relationship and a COVERS relationship, each carrying an `id` that
// names it. The two relationships join the pair in ONE direction, which is the
// condition item 8 names.
func seedParallelPair(t *testing.T, roadmap, specKey, testKey string) {
	t.Helper()
	seedPair(t, roadmap, specKey, testKey,
		"CREATE (a)-[:VERIFIED_BY {id:'verified-by'}]->(b), (a)-[:COVERS {id:'covers'}]->(b)")
}

// seedReversePair creates a Spec node and a Test node joined only test -> spec,
// by an EXERCISES relationship carrying the `id` exercises: joined, but not in the
// direction the statements below create a relationship.
func seedReversePair(t *testing.T, roadmap, specKey, testKey string) {
	t.Helper()
	seedPair(t, roadmap, specKey, testKey, "CREATE (b)-[:EXERCISES {id:'exercises'}]->(a)")
}

// seedPair creates a Spec node and a Test node and, when joining is not empty,
// runs it with the two bound as `a` and `b`.
func seedPair(t *testing.T, roadmap, specKey, testKey, joining string) {
	t.Helper()
	seeds := []string{"CREATE (:Spec {key:'" + specKey + "'}), (:Test {key:'" + testKey + "'})"}
	if joining != "" {
		seeds = append(seeds, pairMatch(specKey, testKey)+joining)
	}
	for _, seed := range seeds {
		if err := runGraphClient([]string{"-r", roadmap, "--query", seed}); err != nil {
			t.Fatalf("seed %q: %v", seed, err)
		}
	}
}

// pairMatch binds the pair's Spec node as `a` and its Test node as `b`.
func pairMatch(specKey, testKey string) string {
	return "MATCH (a:Spec {key:'" + specKey + "'}), (b:Test {key:'" + testKey + "'}) "
}

// pairKeys names the pair a case runs against, so that no case depends on what
// another left behind.
func pairKeys(label string) (specKey, testKey string) {
	return "retention-policy-" + label, "test_retention_policy_" + label + ".py"
}

// pairRelationships returns the properties of every relationship stored
// spec -> test on the pair, keyed by relationship type. Each fixture gives every
// relationship of a pair its own type, so a repeated type is a fixture fault.
func pairRelationships(t *testing.T, roadmap, specKey, testKey string) map[string]map[string]any {
	t.Helper()
	return relationshipsByType(t, roadmap,
		"MATCH (a:Spec {key:'"+specKey+"'})-[e]->(b:Test {key:'"+testKey+"'}) RETURN type(e), properties(e)")
}

// pairReverseRelationships is pairRelationships for the relationships stored
// test -> spec.
func pairReverseRelationships(t *testing.T, roadmap, specKey, testKey string) map[string]map[string]any {
	t.Helper()
	return relationshipsByType(t, roadmap,
		"MATCH (b:Test {key:'"+testKey+"'})-[e]->(a:Spec {key:'"+specKey+"'}) RETURN type(e), properties(e)")
}

// relationshipsByType runs a read returning type(e) and properties(e), and keys
// the rows by type.
func relationshipsByType(t *testing.T, roadmap, query string) map[string]map[string]any {
	t.Helper()
	rows := graphQueryRows(t, roadmap, query)
	byType := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		relType, ok := row[0].(string)
		if !ok {
			t.Fatalf("type(e) is %T, want string: %v", row[0], row)
		}
		props, ok := row[1].(map[string]any)
		if !ok {
			t.Fatalf("properties(e) is %T, want an object: %v", row[1], row)
		}
		if _, repeated := byType[relType]; repeated {
			t.Fatalf("the read holds two %s relationships; every relationship of a fixture pair "+
				"has a type of its own: %v\nquery=%s", relType, rows, query)
		}
		byType[relType] = props
	}
	return byType
}

// requirePairTypes fails the test unless the pair holds exactly the named
// relationship types.
func requirePairTypes(t *testing.T, pair map[string]map[string]any, want ...string) {
	t.Helper()
	got := slices.Sorted(maps.Keys(pair))
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the pair holds the relationships %v, want %v: %s", got, want, mergeBindingStale)
	}
}

// requireOnlyCarrier fails the test unless key is carried, with value want, by
// the relationship of type owner and by no other relationship of the pair.
func requireOnlyCarrier(t *testing.T, pair map[string]map[string]any, owner, key string, want any, why string) {
	t.Helper()
	for relType, props := range pair {
		got, present := props[key]
		switch {
		case relType == owner && (!present || got != want):
			t.Errorf("%s: the %s relationship carries %s = %v (present=%t), want %v: %s",
				why, relType, key, got, present, want, mergeBindingStale)
		case relType != owner && present:
			t.Errorf("%s: the %s relationship, which the statement did not bind, carries %s = %v: %s",
				why, relType, key, got, mergeBindingStale)
		}
	}
}

// requireOnlyRemoval fails the test unless key is absent from the relationship
// of type owner and still present, with its seeded value, on every other one.
func requireOnlyRemoval(t *testing.T, pair map[string]map[string]any, owner, key string,
	seeded map[string]any, why string,
) {
	t.Helper()
	for relType, props := range pair {
		got, present := props[key]
		switch {
		case relType == owner && present:
			t.Errorf("%s: the %s relationship still carries %s = %v: %s",
				why, relType, key, got, mergeBindingStale)
		case relType != owner && (!present || got != seeded[relType]):
			t.Errorf("%s: the %s relationship, which the statement did not bind, carries %s = %v "+
				"(present=%t), want %v: %s", why, relType, key, got, present, seeded[relType],
				mergeBindingStale)
		}
	}
}

// TestGraphMerge_AMatchedBindingActsOnTheRelationshipItMatched is item 8's first
// condition at the pair its earlier form named: a pair joined by several
// relationships in one direction, each MERGE naming one of them by type. Each
// write lands on the relationship its MERGE matched and on no other — the
// reverse of the earlier engine, which bound one relationship of the pair to
// every row whichever one matched.
//
// Two statements are run, one per relationship type, and each through a
// different write form — a trailing `SET` and `ON MATCH SET`, both of which item 8
// names. Neither creates a relationship.
func TestGraphMerge_AMatchedBindingActsOnTheRelationshipItMatched(t *testing.T) {
	const roadmap = "graph-merge-matched-binding"
	defer servedRoadmap(t, roadmap)()
	const specKey, testKey = "release-checklist", "test_release_checklist.py"
	seedParallelPair(t, roadmap, specKey, testKey)

	match := "MATCH (a:Spec {key:'" + specKey + "'}), (b:Test {key:'" + testKey + "'}) "
	for _, statement := range []string{
		match + "MERGE (a)-[e:COVERS]->(b) SET e.merged_via_set = 'commit-7f3a2c1'",
		match + "MERGE (a)-[e:VERIFIED_BY]->(b) ON MATCH SET e.merged_via_on_match = 'commit-7f3a2c1'",
	} {
		out := graphClientJSON(t, roadmap, statement)
		assertCounters(t, out, map[string]int64{"propertiesWritten": 1},
			"a MERGE that matched exactly one relationship of its type and wrote one property to it")
	}

	pair := pairRelationships(t, roadmap, specKey, testKey)
	requirePairTypes(t, pair, "COVERS", "VERIFIED_BY")
	requireOnlyCarrier(t, pair, "COVERS", "merged_via_set", "commit-7f3a2c1",
		"a SET through a MERGE that matched the COVERS relationship")
	requireOnlyCarrier(t, pair, "VERIFIED_BY", "merged_via_on_match", "commit-7f3a2c1",
		"an ON MATCH SET through a MERGE that matched the VERIFIED_BY relationship")
}

// TestGraphMerge_BindingsThatDidNotMatchActOnTheirOwnRelationship is the sound
// side. On a pair joined by several relationships in one direction, a
// relationship bound by MATCH, by CREATE, or by a MERGE that created it takes an
// assignment and a removal on itself; a DELETE through a MATCH binding removes
// that relationship and no other; and a created relationship re-bound with MATCH
// before the DELETE is the one deleted. A DELETE through a created binding
// reaches its own relationship on a pair no relationship joins, and on a pair
// joined only in the other direction; on a pair already joined in its direction,
// TestGraphCreate_ACreatedBindingsDeleteOnAJoinedPairRemovesWhatItCreated
// asserts the same.
//
// Each case runs against a pair of its own, so no case depends on what another
// left behind.
func TestGraphMerge_BindingsThatDidNotMatchActOnTheirOwnRelationship(t *testing.T) {
	const roadmap = "graph-merge-own-binding"
	defer servedRoadmap(t, roadmap)()

	seededIDs := map[string]any{"VERIFIED_BY": "verified-by", "COVERS": "covers"}

	t.Run("a MATCH binding takes an assignment and a removal on itself", func(t *testing.T) {
		const specKey, testKey = "audit-trail", "test_audit_trail.py"
		seedParallelPair(t, roadmap, specKey, testKey)
		bound := "MATCH (a:Spec {key:'" + specKey + "'})-[e:COVERS]->(b:Test {key:'" + testKey + "'}) "

		if err := runGraphClient([]string{"-r", roadmap, "--query",
			bound + "SET e.reviewed_in = 'commit-1b9e4d0'"}); err != nil {
			t.Fatalf("the MATCH-bound assignment failed: %v", err)
		}
		pair := pairRelationships(t, roadmap, specKey, testKey)
		requirePairTypes(t, pair, "COVERS", "VERIFIED_BY")
		requireOnlyCarrier(t, pair, "COVERS", "reviewed_in", "commit-1b9e4d0",
			"an assignment through a MATCH binding")

		if err := runGraphClient([]string{"-r", roadmap, "--query", bound + "REMOVE e.id"}); err != nil {
			t.Fatalf("the MATCH-bound removal failed: %v", err)
		}
		requireOnlyRemoval(t, pairRelationships(t, roadmap, specKey, testKey), "COVERS", "id", seededIDs,
			"a removal through a MATCH binding")
	})

	t.Run("a MATCH binding's DELETE removes nothing it did not bind", func(t *testing.T) {
		const specKey, testKey = "session-timeout", "test_session_timeout.py"
		seedParallelPair(t, roadmap, specKey, testKey)

		if err := runGraphClient([]string{"-r", roadmap, "--query",
			"MATCH (a:Spec {key:'" + specKey + "'})-[e:COVERS]->(b:Test {key:'" + testKey + "'}) DELETE e"}); err != nil {
			t.Fatalf("the MATCH-bound DELETE failed: %v", err)
		}
		pair := pairRelationships(t, roadmap, specKey, testKey)
		requirePairTypes(t, pair, "VERIFIED_BY")
		if got := pair["VERIFIED_BY"]["id"]; got != "verified-by" {
			t.Errorf("the relationship the DELETE did not bind is not the one seeded: id = %v: %s",
				got, mergeBindingStale)
		}
	})

	t.Run("a CREATE binding takes an assignment and a removal on itself", func(t *testing.T) {
		const specKey, testKey = "rate-limiter", "test_rate_limiter.py"
		seedParallelPair(t, roadmap, specKey, testKey)

		if err := runGraphClient([]string{"-r", roadmap, "--query", pairMatch(specKey, testKey) +
			"CREATE (a)-[e:DOCUMENTS {id:'documents', draft:true}]->(b) " +
			"SET e.reviewed_in = 'commit-4c2d8a7' REMOVE e.draft"}); err != nil {
			t.Fatalf("the CREATE-bound write failed: %v", err)
		}
		pair := pairRelationships(t, roadmap, specKey, testKey)
		requirePairTypes(t, pair, "COVERS", "DOCUMENTS", "VERIFIED_BY")
		requireOnlyCarrier(t, pair, "DOCUMENTS", "reviewed_in", "commit-4c2d8a7",
			"an assignment through a CREATE binding")
		if _, present := pair["DOCUMENTS"]["draft"]; present {
			t.Errorf("a removal through a CREATE binding left the created relationship's draft "+
				"property in place: %v: %s", pair["DOCUMENTS"], mergeBindingStale)
		}
		for relType, id := range seededIDs {
			if got := pair[relType]["id"]; got != id {
				t.Errorf("the %s relationship, which the statement did not bind, carries id = %v, "+
					"want %v: %s", relType, got, id, mergeBindingStale)
			}
		}
	})

	t.Run("a MERGE that created its relationship takes an assignment and a removal on itself", func(t *testing.T) {
		const specKey, testKey = "token-refresh", "test_token_refresh.py"
		seedParallelPair(t, roadmap, specKey, testKey)

		out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+
			"MERGE (a)-[e:REFERENCES]->(b) "+
			"ON CREATE SET e.id = 'references', e.draft = true "+
			"SET e.reviewed_in = 'commit-9d0e6f5' REMOVE e.draft")
		counters, _ := countersOfOutput(t, out)
		if created, _ := counters["relationshipsCreated"].(float64); created != 1 {
			t.Fatalf("the MERGE was meant to CREATE its relationship; got %v", out)
		}
		pair := pairRelationships(t, roadmap, specKey, testKey)
		requirePairTypes(t, pair, "COVERS", "REFERENCES", "VERIFIED_BY")
		requireOnlyCarrier(t, pair, "REFERENCES", "reviewed_in", "commit-9d0e6f5",
			"an assignment through a MERGE that created its relationship")
		if got := pair["REFERENCES"]["id"]; got != "references" {
			t.Errorf("ON CREATE SET did not reach the created relationship: id = %v: %s",
				got, mergeBindingStale)
		}
		if _, present := pair["REFERENCES"]["draft"]; present {
			t.Errorf("a removal through a MERGE that created its relationship left the draft "+
				"property in place: %v: %s", pair["REFERENCES"], mergeBindingStale)
		}
		for relType, id := range seededIDs {
			if got := pair[relType]["id"]; got != id {
				t.Errorf("the %s relationship, which the statement did not bind, carries id = %v, "+
					"want %v: %s", relType, got, id, mergeBindingStale)
			}
		}
	})

	t.Run("a created binding's DELETE on a pair no relationship joins removes what it created", func(t *testing.T) {
		for _, deletion := range createdBindingDeletions {
			specKey, testKey := pairKeys("fresh-" + deletion.label)
			seedPair(t, roadmap, specKey, testKey, "")

			out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+deletion.clause)
			assertCounters(t, out, createdAndDeleted, deletion.name+" on a pair no relationship joins")
			if pair := pairRelationships(t, roadmap, specKey, testKey); len(pair) != 0 {
				t.Errorf("%s left %v on a pair no relationship joined: %s",
					deletion.name, slices.Sorted(maps.Keys(pair)), mergeBindingStale)
			}
			if reverse := pairReverseRelationships(t, roadmap, specKey, testKey); len(reverse) != 0 {
				t.Errorf("%s left %v joining the pair the other way: %s",
					deletion.name, slices.Sorted(maps.Keys(reverse)), mergeBindingStale)
			}
		}
	})

	t.Run("a created binding's DELETE on a pair joined only the other way removes what it created", func(t *testing.T) {
		for _, deletion := range createdBindingDeletions {
			specKey, testKey := pairKeys("reverse-" + deletion.label)
			seedReversePair(t, roadmap, specKey, testKey)

			out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+deletion.clause)
			assertCounters(t, out, createdAndDeleted,
				deletion.name+" on a pair joined only in the other direction")
			if pair := pairRelationships(t, roadmap, specKey, testKey); len(pair) != 0 {
				t.Errorf("%s left %v in the direction it created: %s",
					deletion.name, slices.Sorted(maps.Keys(pair)), mergeBindingStale)
			}
			reverse := pairReverseRelationships(t, roadmap, specKey, testKey)
			requirePairTypes(t, reverse, "EXERCISES")
			if got := reverse["EXERCISES"]["id"]; got != "exercises" {
				t.Errorf("%s changed the relationship joining the pair the other way: id = %v: %s",
					deletion.name, got, mergeBindingStale)
			}
		}
	})

	t.Run("a created relationship re-bound with MATCH is the one deleted", func(t *testing.T) {
		specKey, testKey := pairKeys("rebound")
		seedParallelPair(t, roadmap, specKey, testKey)

		out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+
			"CREATE (a)-[e:DOCUMENTS]->(b) WITH DISTINCT a, b MATCH (a)-[f:DOCUMENTS]->(b) DELETE f")
		assertCounters(t, out, createdAndDeleted, "a created relationship re-bound with MATCH and deleted")
		pair := pairRelationships(t, roadmap, specKey, testKey)
		requirePairTypes(t, pair, "COVERS", "VERIFIED_BY")
		for relType, id := range seededIDs {
			if got := pair[relType]["id"]; got != id {
				t.Errorf("the %s relationship, which the statement did not bind, carries id = %v, "+
					"want %v: %s", relType, got, id, mergeBindingStale)
			}
		}
	})
}

// createdAndDeleted is what every statement in createdBindingDeletions reports,
// whichever relationship it deleted.
var createdAndDeleted = map[string]int64{"relationshipsCreated": 1, "relationshipsDeleted": 1}

// createdBindingDeletions are the statement tails that create a relationship
// between the bound `a` and `b` and delete it through the binding the creating
// clause made: directly, projected across a WITH, and through a MERGE that
// creates. None of the pairs they run against holds a DOCUMENTS relationship
// beforehand, so the MERGE creates.
var createdBindingDeletions = []struct{ name, label, clause string }{
	{"a CREATE-bound DELETE", "create", "CREATE (a)-[e:DOCUMENTS]->(b) DELETE e"},
	{"a CREATE-bound DELETE across a WITH", "create-with", "CREATE (a)-[e:DOCUMENTS]->(b) WITH e DELETE e"},
	{"the DELETE of a MERGE that created", "merge", "MERGE (a)-[e:DOCUMENTS]->(b) DELETE e"},
}

// TestGraphCreate_ACreatedBindingsDeleteOnAJoinedPairRemovesWhatItCreated is item
// 8's second condition: on a pair a relationship already joins in the clause's
// direction, a DELETE through a relationship bound by CREATE — directly or across
// a WITH — or by a MERGE that created it deletes the created relationship, and
// every pre-existing relationship of the pair survives with every property it
// held. The earlier engine deleted a pre-existing relationship instead and kept
// the created one, with the same counters.
func TestGraphCreate_ACreatedBindingsDeleteOnAJoinedPairRemovesWhatItCreated(t *testing.T) {
	const roadmap = "graph-create-joined-delete"
	defer servedRoadmap(t, roadmap)()

	seededIDs := map[string]any{"VERIFIED_BY": "verified-by", "COVERS": "covers"}
	for _, deletion := range createdBindingDeletions {
		t.Run(deletion.name, func(t *testing.T) {
			specKey, testKey := pairKeys("joined-" + deletion.label)
			seedParallelPair(t, roadmap, specKey, testKey)

			out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+deletion.clause)
			assertCounters(t, out, createdAndDeleted, deletion.name+" on a joined pair")

			pair := pairRelationships(t, roadmap, specKey, testKey)
			requirePairTypes(t, pair, "COVERS", "VERIFIED_BY")
			for relType, id := range seededIDs {
				if got := pair[relType]["id"]; got != id {
					t.Errorf("the pre-existing %s relationship carries id = %v after %s, want %v: %s",
						relType, got, deletion.name, id, mergeBindingStale)
				}
			}
			if reverse := pairReverseRelationships(t, roadmap, specKey, testKey); len(reverse) != 0 {
				t.Errorf("%s left %v joining the pair the other way: %s",
					deletion.name, slices.Sorted(maps.Keys(reverse)), mergeBindingStale)
			}
		})
	}
}

// parallelTypeFixture is criterion 87's fixture: a Spec node and a Test node
// joined, spec -> test, by two relationships of ONE type told apart by a property
// n of 1 and 2. A pair joined by two relationships of different types cannot
// exercise a MERGE that matches more than one, because a typed MERGE then
// matches one.
func parallelTypeFixture(t *testing.T, roadmap, label string) (specKey, testKey string) {
	t.Helper()
	specKey, testKey = pairKeys("parallel-" + label)
	seedPair(t, roadmap, specKey, testKey, "CREATE (a)-[:TRACES {n:1}]->(b), (a)-[:TRACES {n:2}]->(b)")
	return specKey, testKey
}

// tracesByN reads every relationship stored spec -> test on the pair as its type
// and properties, keyed by its n.
func tracesByN(t *testing.T, roadmap, specKey, testKey string) map[float64]map[string]any {
	t.Helper()
	rows := graphQueryRows(t, roadmap, "MATCH (a:Spec {key:'"+specKey+"'})-[e]->(b:Test {key:'"+testKey+"'}) "+
		"RETURN type(e), properties(e)")
	byN := map[float64]map[string]any{}
	for _, row := range rows {
		props, ok := row[1].(map[string]any)
		if !ok {
			t.Fatalf("properties(e) is %T, want an object: %v", row[1], row)
		}
		n, _ := props["n"].(float64)
		if row[0] != "TRACES" {
			t.Errorf("the pair holds a %v relationship, want TRACES only: %v: %s", row[0], rows, mergeBindingStale)
		}
		if _, repeated := byN[n]; repeated {
			t.Fatalf("the pair holds two relationships with n = %v: %v", n, rows)
		}
		byN[n] = props
	}
	return byN
}

// requireTraces fails the test unless the pair holds exactly the TRACES
// relationships whose n are wantN.
func requireTraces(t *testing.T, got map[float64]map[string]any, why string, wantN ...float64) {
	t.Helper()
	keys := slices.Sorted(maps.Keys(got))
	slices.Sort(wantN)
	if !slices.Equal(keys, wantN) {
		t.Fatalf("%s: the pair holds TRACES relationships with n = %v, want %v: %s", why, keys, wantN, mergeBindingStale)
	}
}

// TestGraphParallelPair_EveryBehaviourIsAssertedOnTheReadBack is acceptance
// criterion 87: the relationship and property-value behaviours an earlier engine
// got wrong, each run on a fresh copy of the fixture and asserted on what a
// following read returns.
func TestGraphParallelPair_EveryBehaviourIsAssertedOnTheReadBack(t *testing.T) {
	const roadmap = "graph-parallel-pair"
	defer servedRoadmap(t, roadmap)()

	t.Run("a MERGE over the bound pair binds each relationship once", func(t *testing.T) {
		specKey, testKey := parallelTypeFixture(t, roadmap, "merge-rows")
		rows := graphQueryRows(t, roadmap, pairMatch(specKey, testKey)+"MERGE (a)-[e:TRACES]->(b) RETURN e.n")
		got := make([]float64, 0, len(rows))
		for _, row := range rows {
			n, _ := row[0].(float64)
			got = append(got, n)
		}
		slices.Sort(got)
		if !slices.Equal(got, []float64{1, 2}) {
			t.Fatalf("the MERGE returned e.n = %v, want one row naming 1 and one naming 2: %s", got, mergeBindingStale)
		}
	})

	t.Run("a SET through the MERGE reaches both relationships once", func(t *testing.T) {
		specKey, testKey := parallelTypeFixture(t, roadmap, "merge-set")
		out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+"MERGE (a)-[e:TRACES]->(b) SET e.x = 1")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 2}, "a SET through a MERGE that matched two")
		traces := tracesByN(t, roadmap, specKey, testKey)
		requireTraces(t, traces, "after the SET", 1, 2)
		for n, props := range traces {
			if props["x"] != float64(1) {
				t.Errorf("the relationship with n = %v carries x = %v, want 1: %s", n, props["x"], mergeBindingStale)
			}
		}
	})

	t.Run("a REMOVE and a DELETE through the MERGE reach every matched relationship and no other", func(t *testing.T) {
		specKey, testKey := parallelTypeFixture(t, roadmap, "merge-remove-delete")
		if err := runGraphClient([]string{"-r", roadmap, "--query", pairMatch(specKey, testKey) +
			"CREATE (a)-[:CITES {n:3}]->(b)"}); err != nil {
			t.Fatalf("adding the relationship the MERGE must not reach: %v", err)
		}
		out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+"MERGE (a)-[e:TRACES]->(b) REMOVE e.n")
		assertCounters(t, out, map[string]int64{"propertiesWritten": 2}, "a REMOVE through a MERGE that matched two")
		rows := graphQueryRows(t, roadmap, "MATCH (a:Spec {key:'"+specKey+"'})-[e]->(b:Test {key:'"+testKey+"'}) "+
			"RETURN type(e), e.n")
		for _, row := range rows {
			switch {
			case row[0] == "TRACES" && row[1] != nil:
				t.Errorf("a matched TRACES relationship still carries n = %v: %s", row[1], mergeBindingStale)
			case row[0] == "CITES" && row[1] != float64(3):
				t.Errorf("the CITES relationship the MERGE did not match carries n = %v, want 3: %s", row[1], mergeBindingStale)
			}
		}

		out = graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+"MERGE (a)-[e:TRACES]->(b) DELETE e")
		assertCounters(t, out, map[string]int64{"relationshipsDeleted": 2}, "a DELETE through a MERGE that matched two")
		rows = graphQueryRows(t, roadmap, "MATCH (a:Spec {key:'"+specKey+"'})-[e]->(b:Test {key:'"+testKey+"'}) "+
			"RETURN type(e), e.n")
		if len(rows) != 1 || rows[0][0] != "CITES" || rows[0][1] != float64(3) {
			t.Errorf("after the DELETE the pair holds %v, want only the CITES relationship with n = 3: %s",
				rows, mergeBindingStale)
		}
	})

	t.Run("a MERGE whose inline map one relationship satisfies creates nothing", func(t *testing.T) {
		specKey, testKey := parallelTypeFixture(t, roadmap, "merge-map")
		out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+"MERGE (a)-[e:TRACES {n:2}]->(b)")
		assertNoCounters(t, out, "a MERGE whose inline map an existing relationship satisfies")
		requireTraces(t, tracesByN(t, roadmap, specKey, testKey), "after the MERGE", 1, 2)
	})

	t.Run("a CREATE-bound DELETE on the joined pair deletes what it created", func(t *testing.T) {
		specKey, testKey := parallelTypeFixture(t, roadmap, "create-delete")
		out := graphClientJSON(t, roadmap, pairMatch(specKey, testKey)+"CREATE (a)-[e:TRACES {n:9}]->(b) DELETE e")
		assertCounters(t, out, map[string]int64{"relationshipsCreated": 1, "relationshipsDeleted": 1, "propertiesWritten": 1},
			"a CREATE-bound DELETE on the joined pair")
		requireTraces(t, tracesByN(t, roadmap, specKey, testKey), "after the CREATE-bound DELETE", 1, 2)
	})

	t.Run("a failed statement that had created a relationship leaves the pair as it found it", func(t *testing.T) {
		specKey, testKey := parallelTypeFixture(t, roadmap, "failed-create")
		var err error
		captureStdStreams(t, func() {
			err = runGraphClient([]string{"-r", roadmap, "--query",
				pairMatch(specKey, testKey) + "CREATE (a)-[:U]->(b) WITH a RETURN 1/0"})
		})
		if err == nil || !strings.HasPrefix(err.Error(), "graph engine error: graph query failed: ") {
			t.Fatalf("the failing statement reported %v, want the parse-or-execution line", err)
		}
		traces := tracesByN(t, roadmap, specKey, testKey)
		requireTraces(t, traces, "after the failed statement", 1, 2)
		for n, props := range traces {
			if len(props) != 1 {
				t.Errorf("the relationship with n = %v carries %v, want exactly its n: rule 9 of "+
					"§ Error Handling and Exit Codes: %s", n, props, mergeBindingStale)
			}
		}
	})

	t.Run("a list holding null is refused and the value held before is unchanged", func(t *testing.T) {
		specKey, _ := parallelTypeFixture(t, roadmap, "null-list")
		if err := runGraphClient([]string{"-r", roadmap, "--query",
			"MATCH (x {key:'" + specKey + "'}) SET x.k = [7, 8]"}); err != nil {
			t.Fatalf("setting the value the refusal must leave in place: %v", err)
		}
		var err error
		captureStdStreams(t, func() {
			err = runGraphClient([]string{"-r", roadmap, "--query",
				"MATCH (x {key:'" + specKey + "'}) SET x.k = [1, null, 2]"})
		})
		if err == nil || !strings.HasPrefix(err.Error(), "graph engine error: graph query failed: ") {
			t.Fatalf("SET x.k = [1, null, 2] reported %v, want the parse-or-execution line", err)
		}
		rows := graphQueryRows(t, roadmap, "MATCH (x {key:'"+specKey+"'}) RETURN x.k")
		if len(rows) != 1 || fmt.Sprint(rows[0][0]) != "[7 8]" {
			t.Errorf("x.k reads back %v after the refusal, want the [7, 8] it held: SPEC/GRAPH.md § What "+
				"Groadmap Does Not Check, item 9: %s", rows, mergeBindingStale)
		}
	})
}
