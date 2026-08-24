// Package aihelp — contract gates for the AuditOperation entity_type member.
//
// SPEC/DATA_FORMATS.md § enums map entry rule 3 requires every value of the
// AuditOperation enum to carry an entity_type member holding TASK or SPRINT: the
// value an audit entry's own entity_type field holds on a row carrying that
// operation. Without it an agent composing an `audit list` filter cannot tell
// whose history the filter returns, and the only thing left to infer it from is
// the operation's name — which SPEC/HELP.md § Audit operation entity-type
// classification rule 1 forbids, because a prefix match cannot notice the day it
// disagrees with the writer.
//
// These gates are the contract-side counterpart of
// TestAuditOperationClassification_IsTotal in internal/models. That one keeps the
// declaration total; these keep the contract's rendering of it faithful and keep
// the member off the enums it must not appear on.
package aihelp

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// enumValueMembers returns value -> raw member for one enum, and reports for
// each value whether the member was PRESENT at all.
//
// Presence is returned separately from the value on purpose, and for the legacy
// member it is the whole point: `false` and absent are different findings that a
// single map cannot tell apart. Absent is the shape every enum but
// AuditOperation must have; `false` is a value AuditOperation must publish.
// Reading the member raw (any, not string or bool) also lets the gates below
// report a null or a wrong JSON type as the distinct defect it is, rather than
// silently reading it as a zero value.
func enumValueMembers(t *testing.T, enums map[string]any, name, member string) (present map[string]bool, values map[string]any) {
	t.Helper()
	def, ok := enums[name].(map[string]any)
	if !ok {
		t.Fatalf("enums.%s missing from contract (present: %v)", name, enumNamesOf(enums))
	}
	raw, ok := def["values"].([]any)
	if !ok {
		t.Fatalf("enums.%s.values wrong type: %T", name, def["values"])
	}
	present = make(map[string]bool, len(raw))
	values = make(map[string]any, len(raw))
	for i, r := range raw {
		entry, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("enums.%s.values[%d] wrong type: %T", name, i, r)
		}
		v, ok := entry["value"].(string)
		if !ok {
			t.Fatalf("enums.%s.values[%d].value missing or not a string", name, i)
		}
		got, declared := entry[member]
		present[v] = declared
		if declared {
			values[v] = got
		}
	}
	return present, values
}

// enumValueDescriptions returns value -> description for one enum. Rule 4
// requires the legacy member and the description to state the same fact, so the
// gate that checks their agreement needs both sides of it.
func enumValueDescriptions(t *testing.T, enums map[string]any, name string) map[string]string {
	t.Helper()
	_, raw := enumValueMembers(t, enums, name, "description")
	out := make(map[string]string, len(raw))
	for v, d := range raw {
		s, ok := d.(string)
		if !ok {
			t.Fatalf("enums.%s value %s has a %T description, want string", name, v, d)
		}
		out[v] = s
	}
	return out
}

// enumValueEntityTypes narrows enumValueMembers to the entity_type member,
// which is a string on every value that carries it.
func enumValueEntityTypes(t *testing.T, enums map[string]any, name string) (present map[string]bool, values map[string]string) {
	t.Helper()
	present, raw := enumValueMembers(t, enums, name, "entity_type")
	values = make(map[string]string, len(raw))
	for v, got := range raw {
		s, ok := got.(string)
		if !ok {
			t.Fatalf("enums.%s value %s has entity_type %T, want string", name, v, got)
		}
		values[v] = s
	}
	return present, values
}

// TestGenerate_AuditOperationValuesCarryTheirEntityType is rule 3: the member is
// present on every value, is one of TASK and SPRINT, and equals what
// internal/models declares. The equality is what makes the contract and the
// audit family help one classification rather than two that can drift.
func TestGenerate_AuditOperationValuesCarryTheirEntityType(t *testing.T) {
	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))
	present, entityTypes := enumValueEntityTypes(t, enums, "AuditOperation")

	checked := 0
	for _, op := range models.ValidAuditOperations {
		name := string(op)
		if !present[name] {
			t.Errorf("enums.AuditOperation %s carries no entity_type member; an agent reading the contract "+
				"cannot tell whose history an `audit list --operation %s` filter returns, and the only "+
				"thing left to infer it from is the operation's name", name, name)
			continue
		}
		got := entityTypes[name]
		if !models.IsValidEntityType(got) {
			t.Errorf("enums.AuditOperation %s publishes entity_type %q, which is not a valid entity type; "+
				"the audit table's CHECK admits exactly TASK and SPRINT", name, got)
			continue
		}
		class, declared := models.ClassifyAuditOperation(op)
		if !declared {
			// Reported by TestAuditOperationClassification_IsTotal; with no
			// declaration there is nothing here to compare against.
			continue
		}
		if got != string(class.EntityType) {
			t.Errorf("enums.AuditOperation %s publishes entity_type %q but internal/models declares %q; "+
				"the contract renders the declaration, so the two cannot disagree",
				name, got, class.EntityType)
			continue
		}
		checked++
	}

	if checked != len(models.ValidAuditOperations) {
		t.Errorf("only %d of %d AuditOperation values were checked against the declaration; a gate that "+
			"stops matching reports success while measuring nothing",
			checked, len(models.ValidAuditOperations))
	}
}

// legacyDescriptionPrefix is the marking rule 2 of
// SPEC/DATA_FORMATS.md § enums map entry puts at the head of a LEGACY value's
// own description. The agreement gate keys on the PREFIX rather than on the word
// appearing anywhere, because a value still in use may one day describe itself as
// replacing a LEGACY operation, and that sentence would say nothing false.
const legacyDescriptionPrefix = "LEGACY."

// TestGenerate_AuditOperationValuesPublishTheirLegacyFlag is rule 4: the member
// is present on every value, is a real boolean rather than null, and equals what
// internal/models declares.
//
// The false half is the one that needs saying. `legacy` is published from a
// *bool precisely so that false survives: a plain bool with `omitempty` would
// drop the member from all 39 operations still in use, leaving a contract in
// which only the LEGACY values carry the key and every other value's status has
// to be inferred from its absence — which is the inference this member exists to
// remove. This gate therefore counts the falses as well as the trues.
func TestGenerate_AuditOperationValuesPublishTheirLegacyFlag(t *testing.T) {
	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))
	present, flags := enumValueMembers(t, enums, "AuditOperation", "legacy")

	trues, falses := 0, 0
	for _, op := range models.ValidAuditOperations {
		name := string(op)
		if !present[name] {
			t.Errorf("enums.AuditOperation %s carries no legacy member; a consumer filtering for the "+
				"operations still in use would have to search the description prose for the word "+
				"LEGACY, which is the coupling rule 4 exists to remove", name)
			continue
		}
		got, ok := flags[name].(bool)
		if !ok {
			t.Errorf("enums.AuditOperation %s publishes legacy as %#v (%T), want a JSON boolean; the "+
				"member is never null", name, flags[name], flags[name])
			continue
		}
		class, declared := models.ClassifyAuditOperation(op)
		if !declared {
			// Reported by TestAuditOperationClassification_IsTotal; with no
			// declaration there is nothing here to compare against.
			continue
		}
		if got != class.Legacy {
			t.Errorf("enums.AuditOperation %s publishes legacy=%t but internal/models declares %t; the "+
				"contract renders the declaration, so the two cannot disagree", name, got, class.Legacy)
			continue
		}
		if got {
			trues++
		} else {
			falses++
		}
	}

	if trues == 0 {
		t.Error("no value published legacy=true, so the four values no command writes are indistinguishable " +
			"from the operations in use")
	}
	if falses == 0 {
		t.Error("no value published legacy=false. Either every operation is legacy — which cannot be — or " +
			"the member is being omitted on the operations still in use, which is exactly what a plain " +
			"bool with omitempty would do and what the *bool exists to prevent")
	}
	if trues+falses != len(models.ValidAuditOperations) {
		t.Errorf("%d of %d values were checked against the declaration; a gate that stops matching "+
			"reports success while measuring nothing", trues+falses, len(models.ValidAuditOperations))
	}
}

// TestGenerate_AuditOperationLegacyFlagAgreesWithItsDescription is the clause of
// rule 4 that the presence gate above cannot reach: "The member and the
// `description` state the same fact and MUST agree."
//
// Two independent statements of one fact can drift, and this pair is unusually
// exposed to it because they exist for different readers — the prefix is prose
// for a human, the member is a field for a machine. A value marked legacy in one
// and not the other is a contract that tells a person and a program opposite
// things about whether an operation is still being recorded. The check runs in
// BOTH directions for that reason: a missing prefix and a spurious one are
// different defects and both are caught here.
func TestGenerate_AuditOperationLegacyFlagAgreesWithItsDescription(t *testing.T) {
	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))
	_, flags := enumValueMembers(t, enums, "AuditOperation", "legacy")
	descriptions := enumValueDescriptions(t, enums, "AuditOperation")

	agreements := 0
	for _, op := range models.ValidAuditOperations {
		name := string(op)
		flag, ok := flags[name].(bool)
		if !ok {
			continue // reported by the presence gate above
		}
		desc, described := descriptions[name]
		if !described || strings.TrimSpace(desc) == "" {
			continue // reported by TestGenerate_EveryEnumValueCarriesADescription
		}
		marked := strings.HasPrefix(desc, legacyDescriptionPrefix)

		switch {
		case flag && !marked:
			t.Errorf("enums.AuditOperation %s publishes legacy=true but its description does not open "+
				"with %q, so a human reading the contract is not told that no command writes it and is "+
				"not told which operations replaced it (rule 2).\n  description: %s",
				name, legacyDescriptionPrefix, desc)
		case !flag && marked:
			t.Errorf("enums.AuditOperation %s publishes legacy=false but its description opens with %q. "+
				"One of the two is wrong: either a command still writes it and the description tells "+
				"readers to stop filtering on a live operation, or nothing writes it and every consumer "+
				"testing the field is misled.\n  description: %s",
				name, legacyDescriptionPrefix, desc)
		default:
			agreements++
		}
	}

	if agreements != len(models.ValidAuditOperations) {
		t.Errorf("%d of %d values were compared against their description; the two statements of the "+
			"LEGACY fact are only as good as the comparison between them",
			agreements, len(models.ValidAuditOperations))
	}

	// Non-vacuity: the biconditional above is satisfied trivially if NOTHING is
	// marked. At least one value must exercise each branch.
	markedCount := 0
	for _, desc := range descriptions {
		if strings.HasPrefix(desc, legacyDescriptionPrefix) {
			markedCount++
		}
	}
	if markedCount == 0 {
		t.Error("no description opens with the LEGACY marking, so the agreement above held without ever " +
			"comparing a marked value")
	}
	if markedCount == len(descriptions) {
		t.Error("every description opens with the LEGACY marking, so the agreement above held without " +
			"ever comparing an unmarked value")
	}
}

// TestGenerate_AuditOnlyMembersAppearOnNoOtherEnum is the converse half of
// rules 3 and 4: "entity_type and legacy appear only where they apply". A TaskStatus value is not
// recorded against an entity at all, so the member must be ABSENT from those
// values rather than present and empty or null — the same convention the
// contract already uses where commands[].flags[] omits range, min_length and
// max_length instead of publishing them as null.
func TestGenerate_AuditOnlyMembersAppearOnNoOtherEnum(t *testing.T) {
	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))

	otherEnums := 0
	for name := range enums {
		if name == "AuditOperation" {
			continue
		}
		present, _ := enumValueEntityTypes(t, enums, name)
		if len(present) == 0 {
			continue
		}
		otherEnums++
		for value, declared := range present {
			if declared {
				t.Errorf("enums.%s value %q carries an entity_type member; the member belongs to "+
					"AuditOperation alone, and publishing it elsewhere suggests the contract has a general "+
					"notion of an enum value's entity, which it does not", name, value)
			}
		}

		// The same half of rule 4 for the legacy member. It is checked in this
		// loop rather than in one of its own because the two members share the
		// rule and would otherwise drift apart: no TaskStatus value is LEGACY,
		// so publishing "legacy": false on one would suggest the contract has a
		// general notion of an enum value's LEGACY status, which it has not.
		legacyPresent, _ := enumValueMembers(t, enums, name, "legacy")
		for value, declared := range legacyPresent {
			if declared {
				t.Errorf("enums.%s value %q carries a legacy member; the member belongs to "+
					"AuditOperation alone", name, value)
			}
		}
	}

	// The contract publishes eight enums today. If the walk ever sees only
	// AuditOperation, this gate is asserting nothing and must say so.
	if otherEnums == 0 {
		t.Fatal("no enum other than AuditOperation was examined, so the 'appears only where it applies' " +
			"half of rule 3 measured nothing")
	}
}
