package commands

// Tests for the relationship-identity hazard published as SPEC/GRAPH.md § What
// Groadmap Does Not Check, item 8, asserted in both directions as mitigation 3 of
// § Dependency Maturity Risk requires.
//
// Item 8 has two conditions, and how the binding was made decides which applies.
//
// A `MERGE` whose relationship pattern joins two nodes the statement has already
// bound, and which MATCHES an existing relationship rather than creating one,
// does not reliably bind that relationship where more than one relationship joins
// the pair in the pattern's direction: every row it emits binds the identifier of
// one relationship of the pair — the same one whichever relationship matched — so
// a write through the binding lands on that relationship.
//
// A relationship bound by `CREATE`, or by a `MERGE` that created it, takes every
// assignment and removal on itself; but on a pair a relationship already joined in
// that direction, `DELETE e` does not reach it: the statement deletes one of the
// pair's pre-existing relationships and keeps the one it created, and the write
// counters report one created and one deleted either way.
//
// No error is raised and every statement reports success. Both sides are
// asserted, because an assertion of the losing side alone is satisfied by an
// engine whose working side has regressed to match it: a relationship bound by
// MATCH takes every write, removal and deletion on itself; a created binding's
// deletion reaches its own relationship on a pair no other relationship joins in
// that direction; and re-binding the created relationship with MATCH before the
// DELETE deletes it on any pair.
//
// Every joined fixture joins its pair by two relationships of DIFFERENT types in
// one direction. The type is what makes "the relationship the MERGE matched" and
// "the relationship the CREATE made" facts rather than guesses. Which
// relationship of the pair the engine binds or deletes instead is not specified,
// so it is not asserted; what is asserted is what the specification states.
//
// Every read-back uses an outgoing MATCH, which SPEC/GRAPH.md § What Groadmap
// Does Not Check, item 5, states is resolved correctly whatever the data.

import (
	"maps"
	"slices"
	"testing"
)

// mergeBindingStale is the reason every failure below names.
const mergeBindingStale = "the engine's behaviour has moved and SPEC/GRAPH.md § What Groadmap Does Not " +
	"Check, item 8, no longer describes it; the item and mitigation 3 must be corrected"

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

// TestGraphMerge_AMatchedBindingOnAParallelPairActsOnAnotherRelationship is the
// hazard side: a write through a relationship bound by a MERGE that matched one of
// several relationships joining the pair in one direction reaches a relationship
// other than the one matched.
//
// Two statements are run, one per relationship type, and each through a
// different write form — a trailing `SET` and `ON MATCH SET`, both of which item 8
// names. Neither creates a relationship, so each matched the one relationship of
// its type; and both writes land on one relationship, so one of them landed on a
// relationship its MERGE did not match.
func TestGraphMerge_AMatchedBindingOnAParallelPairActsOnAnotherRelationship(t *testing.T) {
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
		if ok, _ := out["ok"].(bool); !ok {
			t.Fatalf("the statement must report success: %q gave %v", statement, out)
		}
		counters, _ := countersOfOutput(t, out)
		if _, created := counters["relationshipsCreated"]; created {
			t.Fatalf("the MERGE was meant to MATCH an existing relationship and created one: %q gave %v",
				statement, out)
		}
	}

	pair := pairRelationships(t, roadmap, specKey, testKey)
	requirePairTypes(t, pair, "COVERS", "VERIFIED_BY")

	carriers := func(key string) []string {
		var found []string
		for relType, props := range pair {
			if _, present := props[key]; present {
				found = append(found, relType)
			}
		}
		slices.Sort(found)
		return found
	}
	viaSet, viaOnMatch := carriers("merged_via_set"), carriers("merged_via_on_match")
	if len(viaSet) != 1 || len(viaOnMatch) != 1 {
		t.Fatalf("each write must land on exactly one relationship; merged_via_set is on %v and "+
			"merged_via_on_match on %v: %s", viaSet, viaOnMatch, mergeBindingStale)
	}
	if viaSet[0] != viaOnMatch[0] {
		t.Fatalf("each MERGE wrote to its own relationship (merged_via_set on %s, merged_via_on_match "+
			"on %s): a matched MERGE on a pair joined by several relationships in one direction is "+
			"specified to bind the SAME relationship whichever one matched: %s",
			viaSet[0], viaOnMatch[0], mergeBindingStale)
	}
	// Both writes are on one relationship, so the MERGE whose type is not that
	// relationship's wrote to a relationship it did not match.
	misdirected := "the COVERS MERGE"
	if viaSet[0] == "COVERS" {
		misdirected = "the VERIFIED_BY MERGE"
	}
	t.Logf("both writes landed on the %s relationship; %s wrote to a relationship it did not match",
		viaSet[0], misdirected)
}

// TestGraphMerge_BindingsThatDidNotMatchActOnTheirOwnRelationship is the sound
// side. On a pair joined by several relationships in one direction, a
// relationship bound by MATCH, by CREATE, or by a MERGE that created it takes an
// assignment and a removal on itself; a DELETE through a MATCH binding removes
// that relationship and no other; and a created relationship re-bound with MATCH
// before the DELETE is the one deleted. A DELETE through a created binding
// reaches its own relationship on a pair no relationship joins, and on a pair
// joined only in the other direction. No case asserts that a DELETE through a
// created binding is sound on a pair already joined in its direction: item 8
// states that it is not, and
// TestGraphCreate_ACreatedBindingsDeleteOnAJoinedPairRemovesAnOlderRelationship
// asserts that.
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

// TestGraphCreate_ACreatedBindingsDeleteOnAJoinedPairRemovesAnOlderRelationship
// is the hazard side of item 8's second condition: on a pair a relationship
// already joined in the clause's direction, a DELETE through a relationship bound
// by CREATE — directly or across a WITH — or by a MERGE that created it keeps the
// created relationship and deletes one of the pair's pre-existing ones, and the
// counters report one created and one deleted, exactly as they would for the
// right deletion.
func TestGraphCreate_ACreatedBindingsDeleteOnAJoinedPairRemovesAnOlderRelationship(t *testing.T) {
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
			if len(pair) != 2 {
				t.Fatalf("the pair holds %v after the statement; want the created DOCUMENTS "+
					"relationship and one of the two it held before: %s",
					slices.Sorted(maps.Keys(pair)), mergeBindingStale)
			}
			if _, kept := pair["DOCUMENTS"]; !kept {
				t.Fatalf("%s deleted the relationship it created, and the pair holds %v: a "+
					"created binding's DELETE on a joined pair is specified to keep it: %s",
					deletion.name, slices.Sorted(maps.Keys(pair)), mergeBindingStale)
			}
			survivors := 0
			for relType, id := range seededIDs {
				props, present := pair[relType]
				if !present {
					continue
				}
				survivors++
				if props["id"] != id {
					t.Errorf("the surviving %s relationship carries id = %v, want %v: %s",
						relType, props["id"], id, mergeBindingStale)
				}
			}
			if survivors != 1 {
				t.Errorf("%d of the pair's two pre-existing relationships survived; exactly one is "+
					"specified to be deleted in place of the created one: %s", survivors, mergeBindingStale)
			}
			if reverse := pairReverseRelationships(t, roadmap, specKey, testKey); len(reverse) != 0 {
				t.Errorf("%s left %v joining the pair the other way: %s",
					deletion.name, slices.Sorted(maps.Keys(reverse)), mergeBindingStale)
			}
		})
	}
}
