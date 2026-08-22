// Package models — gates over the audit operation classification.
//
// SPEC/HELP.md § Audit operation entity-type classification rule 5(a) requires a
// gate that fails when any catalogue value has no declared entity type, and
// requires it to live where the operation constants are declared. That placement
// is the point: the person who adds an AuditOperation constant gets the failure
// from the package they just edited, on the first `go test ./internal/models/`
// they run, rather than from a help renderer two packages away.
//
// Why this gate and not a coverage gate over the help: the `Valid operations`
// block is rendered from ValidAuditOperations, so a newly declared value is
// printed the moment it is declared and no coverage gate over the block can fail
// on it. Rule 5 states that limit explicitly and splits the remedy in two — this
// file is (a), and TestAuditOperationBlock_HasNoCatchAllGroup in
// internal/commands is (b). Neither reduces to the other: (a) makes the omission
// fail, (b) removes the group that would otherwise let an unclassified operation
// be printed under a heading asserting nothing about it.
package models

import "testing"

// TestAuditOperationClassification_IsTotal is rule 5(a). Every value the
// catalogue holds must carry exactly one declared entity type, LEGACY values
// included, because entity_type is NOT NULL on the audit table and its CHECK
// admits exactly TASK and SPRINT: an operation with no entity type would
// describe rows that cannot exist (rule 3).
func TestAuditOperationClassification_IsTotal(t *testing.T) {
	for _, op := range ValidAuditOperations {
		class, declared := ClassifyAuditOperation(op)
		if !declared {
			t.Errorf("%s is in ValidAuditOperations but has no entry in auditOperationClasses, so both "+
				"published surfaces would have to guess the entity its rows belong to. Add "+
				"\"%s: {EntityType: <EntityTask|EntitySprint>}\" to auditOperationClasses in audit.go, "+
				"choosing the value by observing a row the writer produces — not by reading the name", op, op)
			continue
		}
		if !IsValidEntityType(string(class.EntityType)) {
			t.Errorf("%s is declared against entity type %q, which is not a valid EntityType; the audit "+
				"table's CHECK admits exactly TASK and SPRINT", op, class.EntityType)
		}
	}
}

// TestAuditOperationClassification_DeclaresNothingUncatalogued is the converse
// direction. Without it the declaration could keep an entry for a constant that
// was removed from the catalogue, and the totality gate above would stay green
// while a published surface carried a classification for an operation that no
// longer exists.
func TestAuditOperationClassification_DeclaresNothingUncatalogued(t *testing.T) {
	for op := range auditOperationClasses {
		if !IsValidAuditOperation(string(op)) {
			t.Errorf("auditOperationClasses classifies %s, which is not in ValidAuditOperations, so no "+
				"code path can write it and `audit list --operation %s` is rejected; the constant was "+
				"probably retired and its classification outlived it", op, op)
		}
	}
	if len(auditOperationClasses) != len(ValidAuditOperations) {
		t.Errorf("auditOperationClasses has %d entries and ValidAuditOperations declares %d; with both "+
			"coverage directions satisfied the difference can only come from a duplicate inside "+
			"ValidAuditOperations", len(auditOperationClasses), len(ValidAuditOperations))
	}
}

// TestAuditOperationClassification_LegacyFlagsMatchTheLegacyGroup ties the
// Legacy member to legacyAuditOperations, the list the catalogue-order gates in
// this package already use. Two independent statements of "which operations are
// legacy" can disagree; this makes the disagreement a failure rather than a
// help text and a contract that describe the same operation differently.
func TestAuditOperationClassification_LegacyFlagsMatchTheLegacyGroup(t *testing.T) {
	legacy := make(map[AuditOperation]bool, len(legacyAuditOperations))
	for _, op := range legacyAuditOperations {
		legacy[op] = true
		class, declared := ClassifyAuditOperation(op)
		if !declared {
			continue // reported by the totality gate above
		}
		if !class.Legacy {
			t.Errorf("%s is a member of the catalogue's LEGACY group but auditOperationClasses does not "+
				"mark it Legacy, so both surfaces would list it indistinguishably from the operations in "+
				"use and a reader filtering for current activity would get an empty result", op)
		}
	}
	for _, op := range ValidAuditOperations {
		class, declared := ClassifyAuditOperation(op)
		if !declared || !class.Legacy {
			continue
		}
		if !legacy[op] {
			t.Errorf("auditOperationClasses marks %s Legacy but it is not in legacyAuditOperations; a "+
				"command still writes it, so publishing it as legacy tells a reader to stop filtering on "+
				"an operation that is still being recorded", op)
		}
	}
}

// TestAuditOperationClassification_EveryEntityTypeIsPopulated guards against the
// classification collapsing onto one entity type — the shape a careless
// find-and-replace produces. It is not a claim about how many operations each
// entity should have, only that neither group is empty, because an empty group
// would silently remove a whole entity's operations from the help.
func TestAuditOperationClassification_EveryEntityTypeIsPopulated(t *testing.T) {
	counts := map[EntityType]int{}
	for _, op := range ValidAuditOperations {
		if class, declared := ClassifyAuditOperation(op); declared {
			counts[class.EntityType]++
		}
	}
	for _, entity := range []EntityType{EntityTask, EntitySprint} {
		if counts[entity] == 0 {
			t.Errorf("no operation is declared against %s, so the %s group of the audit help would render "+
				"empty and every operation would be attributed to the other entity", entity, entity)
		}
	}
}
